package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/config"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/iap"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

const maxIAPTransactionBodySize = 16 * 1024 // 16 KiB (contract Section 2 / 4.2)

// ProcessTransactionRequestDTO represents the student's submission of a StoreKit 2 transaction JWS.
type ProcessTransactionRequestDTO struct {
	SignedTransaction string `json:"signed_transaction"`
}

// IAPTransactionResponseDTO represents the successful result of verifying and processing a transaction.
type IAPTransactionResponseDTO struct {
	Status            string              `json:"status"`
	TransactionID     string              `json:"transaction_id"`
	ProductID         string              `json:"product_id"`
	PackageID         string              `json:"package_id"`
	GrantedSubjectIDs []string            `json:"granted_subject_ids"`
	Skipped           []models.IAPSkipped `json:"skipped"`
}

// ProcessIAPTransaction handles POST /academy/iap/transactions.
// Executes the check order mandated by Decision D3 (2026-11-14):
// 1. web token -> 403, body size (16 KiB) / JSON validation -> 400
// 2. JWS signature + chain + OIDs -> 422 invalid_transaction
// 3. bundleId -> 422 wrong_bundle; type -> 422 unsupported_product_type
// 4. appAccountToken == caller's token -> else 403 account_mismatch
// 5. idempotency lookup by transaction_id: same user -> 200 already_processed, other user -> 409
// 6. revocationDate -> 422 revoked_transaction
// 7. Sandbox allowlist -> 403 sandbox_not_allowed
// 8. product -> package else 404 unknown_product
// 9. grant + insert purchase (concurrency safe via unique index on transaction_id)
func (s *Server) ProcessIAPTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	// 1. Authentication and client check
	claims := StudentClaims(r)
	if claims == nil || claims.UserID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	if claims.Client == "web" {
		handlerutil.WriteSafeError(w, r, http.StatusForbidden, "app_client_required", "هذه الميزة متاحة فقط من تطبيق الهاتف", nil)
		return
	}

	// Body size cap (16 KiB) and strict JSON decoding
	r.Body = http.MaxBytesReader(w, r.Body, maxIAPTransactionBodySize)
	var req ProcessTransactionRequestDTO
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_body", "invalid request body", err)
		return
	}
	if strings.TrimSpace(req.SignedTransaction) == "" {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_body", "signed_transaction is required", nil)
		return
	}

	// 2. JWS signature + certificate chain + Apple marker OIDs
	verifier := s.IAPVerifier
	if verifier == nil {
		verifier = iap.DefaultVerifier()
	}
	payload, err := verifier.Verify(req.SignedTransaction)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnprocessableEntity, "invalid_transaction", "transaction signature or chain verification failed", err)
		return
	}

	// 3. bundleId and product type
	expectedBundle := s.IAPBundleID
	if expectedBundle == "" {
		expectedBundle = config.DefaultIAPBundleID
	}
	if payload.BundleID != expectedBundle {
		handlerutil.WriteSafeError(w, r, http.StatusUnprocessableEntity, "wrong_bundle", fmt.Sprintf("bundleId %q does not match expected %q", payload.BundleID, expectedBundle), nil)
		return
	}
	if payload.Type != "Non-Renewing Subscription" {
		handlerutil.WriteSafeError(w, r, http.StatusUnprocessableEntity, "unsupported_product_type", fmt.Sprintf("product type %q is not supported", payload.Type), nil)
		return
	}

	// 4. appAccountToken == student's deterministic token
	expectedToken := models.DeterministicAppAccountToken(claims.UserID)
	if payload.AppAccountToken == "" || !strings.EqualFold(payload.AppAccountToken, expectedToken) {
		handlerutil.WriteSafeError(w, r, http.StatusForbidden, "account_mismatch", "appAccountToken does not match student account", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	// 5. Idempotency lookup by transaction_id
	// Stored result is returned immediately with NO further checks (revocation/sandbox/package checks are bypassed).
	existing, err := s.Store.GetIAPPurchaseByTransactionID(dbCtx, payload.TransactionID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, "iap_unavailable", "database unavailable", err)
		return
	}
	if existing != nil {
		if existing.UserID == claims.UserID {
			resp := IAPTransactionResponseDTO{
				Status:            "already_processed",
				TransactionID:     existing.TransactionID,
				ProductID:         existing.ProductID,
				PackageID:         existing.PackageID,
				GrantedSubjectIDs: existing.GrantedSubjectIDs,
				Skipped:           existing.Skipped,
			}
			if resp.GrantedSubjectIDs == nil {
				resp.GrantedSubjectIDs = []string{}
			}
			if resp.Skipped == nil {
				resp.Skipped = []models.IAPSkipped{}
			}
			s.logIAPOutcome(claims.UserID, payload.TransactionID, payload.ProductID, "already_processed")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		// Different student -> 409 conflict
		s.logIAPOutcome(claims.UserID, payload.TransactionID, payload.ProductID, "transaction_owned_by_other_user")
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "transaction_owned_by_other_user", "transaction was processed by another account", nil)
		return
	}

	// 6. revocationDate check
	if payload.RevocationDate > 0 {
		s.logIAPOutcome(claims.UserID, payload.TransactionID, payload.ProductID, "revoked_transaction")
		handlerutil.WriteSafeError(w, r, http.StatusUnprocessableEntity, "revoked_transaction", "transaction has been revoked or refunded", nil)
		return
	}

	// 7. Sandbox allowlist check
	if payload.Environment == "Sandbox" {
		allowed := s.IAPSandboxAllowAll
		if !allowed {
			for _, uid := range s.IAPSandboxAllowedUserIDs {
				if uid == claims.UserID {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			s.logIAPOutcome(claims.UserID, payload.TransactionID, payload.ProductID, "sandbox_not_allowed")
			handlerutil.WriteSafeError(w, r, http.StatusForbidden, "sandbox_not_allowed", "sandbox transactions not permitted for this user", nil)
			return
		}
	}

	// 8. Product -> Package lookup (honours valid transactions regardless of published or available_until per owner decision 7)
	pkg, err := s.Store.GetIAPPackageByProductID(dbCtx, payload.ProductID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.logIAPOutcome(claims.UserID, payload.TransactionID, payload.ProductID, "unknown_product")
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, "unknown_product", fmt.Sprintf("no package found for product_id %q", payload.ProductID), nil)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, "iap_unavailable", "database unavailable", err)
		return
	}
	if pkg == nil {
		s.logIAPOutcome(claims.UserID, payload.TransactionID, payload.ProductID, "unknown_product")
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "unknown_product", fmt.Sprintf("no package found for product_id %q", payload.ProductID), nil)
		return
	}

	// 9. Grant subjects and record purchase
	now := time.Now().UTC()
	var grantedIDs []string
	var skipped []models.IAPSkipped

	for _, sid := range pkg.SubjectIDs {
		subj, err := s.Store.GetSubjectByID(dbCtx, sid)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, "iap_unavailable", "database unavailable", err)
			return
		}
		if subj == nil || subj.Status != models.StatusPublished {
			skipped = append(skipped, models.IAPSkipped{SubjectID: sid, Reason: "unavailable"})
			continue
		}
		lvl, err := s.Store.GetLevelByKey(dbCtx, subj.LevelKey)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, "iap_unavailable", "database unavailable", err)
			return
		}
		if lvl == nil || !lvl.Published {
			skipped = append(skipped, models.IAPSkipped{SubjectID: sid, Reason: "unavailable"})
			continue
		}

		ent := &models.Entitlement{
			UserID:    claims.UserID,
			SubjectID: sid,
			ExpiresAt: subj.AccessExpiresAt,
			GrantedAt: now,
			Source:    models.EntitlementSourceIAP,
			GrantedBy: "apple_iap:" + payload.TransactionID,
			Active:    true,
		}
		err = s.Store.Grant(dbCtx, ent)
		if err == nil {
			grantedIDs = append(grantedIDs, sid)
		} else if errors.Is(err, store.ErrDuplicate) {
			skipped = append(skipped, models.IAPSkipped{SubjectID: sid, Reason: "already_owned"})
		} else if errors.Is(err, store.ErrSubjectExpired) {
			skipped = append(skipped, models.IAPSkipped{SubjectID: sid, Reason: "expired"})
		} else if errors.Is(err, store.ErrNotFound) {
			skipped = append(skipped, models.IAPSkipped{SubjectID: sid, Reason: "unavailable"})
		} else {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, "iap_unavailable", "grant failed", err)
			return
		}
	}

	if grantedIDs == nil {
		grantedIDs = []string{}
	}
	if skipped == nil {
		skipped = []models.IAPSkipped{}
	}

	purUUID, err := jwtutil.GenerateUUID()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "failed to generate purchase id", err)
		return
	}
	purchaseID := "pur_" + strings.ReplaceAll(purUUID, "-", "")[:16]

	purchaseDate := time.UnixMilli(payload.PurchaseDate).UTC()
	purchase := &models.IAPPurchase{
		ID:                    purchaseID,
		TransactionID:         payload.TransactionID,
		OriginalTransactionID: payload.OriginalTransactionID,
		UserID:                claims.UserID,
		PackageID:             pkg.ID,
		ProductID:             payload.ProductID,
		Environment:           payload.Environment,
		PurchaseDate:          purchaseDate,
		GrantedSubjectIDs:     grantedIDs,
		Skipped:               skipped,
		CreatedAt:             now,
	}

	err = s.Store.InsertIAPPurchase(dbCtx, purchase)
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			// Concurrency safety: if another goroutine or request concurrently inserted the purchase
			// for the same transaction_id, query the winner's record and return already_processed.
			winner, rErr := s.Store.GetIAPPurchaseByTransactionID(dbCtx, payload.TransactionID)
			if rErr == nil && winner != nil && winner.UserID == claims.UserID {
				resp := IAPTransactionResponseDTO{
					Status:            "already_processed",
					TransactionID:     winner.TransactionID,
					ProductID:         winner.ProductID,
					PackageID:         winner.PackageID,
					GrantedSubjectIDs: winner.GrantedSubjectIDs,
					Skipped:           winner.Skipped,
				}
				s.logIAPOutcome(claims.UserID, payload.TransactionID, payload.ProductID, "already_processed")
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, "iap_unavailable", "failed to record purchase", err)
		return
	}

	s.logIAPOutcome(claims.UserID, payload.TransactionID, payload.ProductID, "granted")
	// Owner 2026-11-14: a new grant (never already_processed) is written to
	// the academy audit log. The JWS and appAccountToken are never recorded;
	// an audit failure is logged and never fails the purchase.
	s.writeIAPPurchaseAudit(r.Context(), claims.UserID, pkg.ID, purchase)
	resp := IAPTransactionResponseDTO{
		Status:            "granted",
		TransactionID:     purchase.TransactionID,
		ProductID:         purchase.ProductID,
		PackageID:         purchase.PackageID,
		GrantedSubjectIDs: purchase.GrantedSubjectIDs,
		Skipped:           purchase.Skipped,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// logIAPOutcome writes sanitized transaction audit logs.
// Neither the JWS string nor the appAccountToken is ever logged.
func (s *Server) logIAPOutcome(userID, txID, productID, outcome string) {
	cleanUID := strings.ReplaceAll(strings.ReplaceAll(userID, "\n", ""), "\r", "")
	cleanTxID := strings.ReplaceAll(strings.ReplaceAll(txID, "\n", ""), "\r", "")
	cleanProdID := strings.ReplaceAll(strings.ReplaceAll(productID, "\n", ""), "\r", "")
	cleanOutcome := strings.ReplaceAll(strings.ReplaceAll(outcome, "\n", ""), "\r", "")
	// #nosec G706 -- fields sanitized of CR/LF; JWS and appAccountToken are never passed
	log.Printf("[IAP] user_id=%s transaction_id=%s product_id=%s outcome=%s", cleanUID, cleanTxID, cleanProdID, cleanOutcome)
}

// writeIAPPurchaseAudit records one "iap_purchase" admin_audit_log entry for
// a new grant. The actor is the student (claims carry no display name, so the
// user id is used as the name); the target is the package. Detail carries
// only product id, environment, transaction id and the granted subject count.
func (s *Server) writeIAPPurchaseAudit(ctx context.Context, userID, packageID string, p *models.IAPPurchase) {
	id, err := jwtutil.GenerateUUID()
	if err == nil {
		entry := &models.AuditLog{
			ID:         id,
			ActorID:    userID,
			ActorName:  userID,
			Action:     "iap_purchase",
			TargetType: "iap_package",
			TargetID:   packageID,
			Detail: sanitizeDetail(fmt.Sprintf("product=%s env=%s tx=%s subjects=%d",
				p.ProductID, p.Environment, p.TransactionID, len(p.GrantedSubjectIDs))),
			CreatedAt: time.Now().UTC(),
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dbTimeout)
		defer cancel()
		err = s.Store.CreateAuditLog(dbCtx, entry)
	}
	if err != nil {
		cleanUID := strings.ReplaceAll(strings.ReplaceAll(userID, "\r", ""), "\n", "")
		cleanPkg := strings.ReplaceAll(strings.ReplaceAll(packageID, "\r", ""), "\n", "")
		// #nosec G706 -- fields sanitized of CR/LF; no JWS or token is logged
		log.Printf("[ERROR] audit log write failed for action iap_purchase actor_id=%s target_id=%s: %v", cleanUID, cleanPkg, err)
	}
}
