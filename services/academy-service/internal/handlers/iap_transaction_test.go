package handlers

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/iap"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

type iapTestCA struct {
	rootKey   *ecdsa.PrivateKey
	rootCert  *x509.Certificate
	interKey  *ecdsa.PrivateKey
	interCert *x509.Certificate
	leafKey   *ecdsa.PrivateKey
	leafCert  *x509.Certificate
	verifier  *iap.Verifier
}

func newIAPTestCA(t *testing.T) *iapTestCA {
	t.Helper()
	now := time.Now().UTC()

	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate root key: %v", err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(10),
		Subject: pkix.Name{
			CommonName:   "Apple Root CA - G3",
			Organization: []string{"Apple Inc."},
		},
		NotBefore:             now.Add(-48 * time.Hour),
		NotAfter:              now.Add(3650 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create root cert: %v", err)
	}
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatalf("parse root cert: %v", err)
	}

	interKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate inter key: %v", err)
	}
	interTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(20),
		Subject: pkix.Name{
			CommonName:   "Apple Worldwide Developer Relations Certification Authority",
			Organization: []string{"Apple Inc."},
		},
		NotBefore:             now.Add(-24 * time.Hour),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtraExtensions: []pkix.Extension{
			{
				Id:    asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 2, 1},
				Value: []byte{0x05, 0x00},
			},
		},
	}
	interDER, err := x509.CreateCertificate(rand.Reader, interTemplate, rootCert, &interKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create inter cert: %v", err)
	}
	interCert, err := x509.ParseCertificate(interDER)
	if err != nil {
		t.Fatalf("parse inter cert: %v", err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(30),
		Subject: pkix.Name{
			CommonName:   "Mac App Store and iTunes Store Receipt Signing",
			Organization: []string{"Apple Inc."},
		},
		NotBefore: now.Add(-1 * time.Hour),
		NotAfter:  now.Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtraExtensions: []pkix.Extension{
			{
				Id:    asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 11, 1},
				Value: []byte{0x05, 0x00},
			},
		},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, interCert, &leafKey.PublicKey, interKey)
	if err != nil {
		t.Fatalf("create leaf cert: %v", err)
	}
	leafCert, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatalf("parse leaf cert: %v", err)
	}

	verifier, err := iap.NewVerifier(rootDER, "")
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	return &iapTestCA{
		rootKey:   rootKey,
		rootCert:  rootCert,
		interKey:  interKey,
		interCert: interCert,
		leafKey:   leafKey,
		leafCert:  leafCert,
		verifier:  verifier,
	}
}

func (ca *iapTestCA) makeJWS(t *testing.T, payload *iap.JWSTransactionPayload) string {
	t.Helper()

	header := struct {
		Alg string   `json:"alg"`
		X5c []string `json:"x5c"`
	}{
		Alg: "ES256",
		X5c: []string{
			base64.StdEncoding.EncodeToString(ca.leafCert.Raw),
			base64.StdEncoding.EncodeToString(ca.interCert.Raw),
			base64.StdEncoding.EncodeToString(ca.rootCert.Raw),
		},
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	signingInput := headerB64 + "." + payloadB64
	digest := sha256.Sum256([]byte(signingInput))

	r, s, err := ecdsa.Sign(rand.Reader, ca.leafKey, digest[:])
	if err != nil {
		t.Fatalf("sign digest: %v", err)
	}

	sigBytes := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)

	sigB64 := base64.RawURLEncoding.EncodeToString(sigBytes)
	return signingInput + "." + sigB64
}

func postTransaction(t *testing.T, s *Server, token, gatewaySecret string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var bodyBytes []byte
	switch v := body.(type) {
	case []byte:
		bodyBytes = v
	case string:
		bodyBytes = []byte(v)
	default:
		var err error
		bodyBytes, err = json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/academy/iap/transactions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if gatewaySecret != "" {
		req.Header.Set("X-Gateway-Secret", gatewaySecret)
	}

	rec := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(rec, req)
	return rec
}

func TestIAPTransaction_AuthAndBodyValidation(t *testing.T) {
	s := newTestServer(false)
	studentID := "usr_stud_val_1"
	studentTok := makeStudentToken(t, studentID)
	webTok := makeWebStudentToken(t, studentID)

	t.Run("Missing_Gateway_Secret_401", func(t *testing.T) {
		rec := postTransaction(t, s, studentTok, "", map[string]string{"signed_transaction": "dummy"})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("got status %d, want 401", rec.Code)
		}
	})

	t.Run("Missing_Student_Token_401", func(t *testing.T) {
		rec := postTransaction(t, s, "", "test-gateway-secret", map[string]string{"signed_transaction": "dummy"})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("got status %d, want 401", rec.Code)
		}
	})

	t.Run("Web_Client_Forbidden_403_AppClientRequired", func(t *testing.T) {
		rec := postTransaction(t, s, webTok, "test-gateway-secret", map[string]string{"signed_transaction": "dummy"})
		if rec.Code != http.StatusForbidden {
			t.Errorf("got status %d, want 403", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "app_client_required") {
			t.Errorf("expected app_client_required, got: %s", rec.Body.String())
		}
	})

	t.Run("Body_Too_Large_Over_16KiB_400", func(t *testing.T) {
		largePayload := strings.Repeat("A", 17*1024)
		rec := postTransaction(t, s, studentTok, "test-gateway-secret", `{"signed_transaction":"`+largePayload+`"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("got status %d, want 400", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "invalid_body") {
			t.Errorf("expected invalid_body error, got %s", rec.Body.String())
		}
	})

	t.Run("DisallowUnknownFields_400", func(t *testing.T) {
		body := `{"signed_transaction":"abc","unknown_extra_field":"val"}`
		rec := postTransaction(t, s, studentTok, "test-gateway-secret", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("got status %d, want 400", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "invalid_body") {
			t.Errorf("expected invalid_body, got: %s", rec.Body.String())
		}
	})

	t.Run("Empty_Signed_Transaction_400", func(t *testing.T) {
		rec := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{"signed_transaction": "   "})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("got status %d, want 400", rec.Code)
		}
	})
}

func TestIAPTransaction_CheckOrder_D3(t *testing.T) {
	s := newTestServer(false)
	ca := newIAPTestCA(t)
	s.IAPVerifier = ca.verifier
	s.IAPBundleID = "com.wael.app"
	s.IAPSandboxAllowedUserIDs = []string{"usr_allowed_sandbox"}
	s.IAPSandboxAllowAll = false

	ctx := context.Background()
	studentID := "usr_allowed_sandbox"
	studentTok := makeStudentToken(t, studentID)
	tokenAccount := models.DeterministicAppAccountToken(studentID)

	future := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	s1 := createIAPTestSubject(t, s, "subj_tx_1", "bachelor-y1", models.StatusPublished, future)

	pkg := &models.IAPPackage{
		ID:         "pkg_tx_test",
		ProductID:  "com.wael.app.pkg.year1",
		TitleAr:    "باقة فحص الطلبات",
		TitleEn:    "Order Check Package",
		SubjectIDs: []string{s1.ID},
		Published:  true,
		Position:   1,
	}
	if err := s.Store.CreateIAPPackage(ctx, pkg); err != nil {
		t.Fatalf("CreateIAPPackage: %v", err)
	}

	basePayload := func(txID string) *iap.JWSTransactionPayload {
		ms := time.Now().UTC().UnixMilli()
		return &iap.JWSTransactionPayload{
			TransactionID:         txID,
			OriginalTransactionID: txID,
			BundleID:              "com.wael.app",
			ProductID:             "com.wael.app.pkg.year1",
			PurchaseDate:          ms,
			OriginalPurchaseDate:  ms,
			SignedDate:            ms,
			Type:                  "Non-Renewing Subscription",
			Environment:           "Sandbox",
			AppAccountToken:       tokenAccount,
		}
	}

	t.Run("Step2_Invalid_JWS_422", func(t *testing.T) {
		rec := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
			"signed_transaction": "header.payload.invalidsig",
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("got status %d, want 422", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "invalid_transaction") {
			t.Errorf("expected invalid_transaction, got %s", rec.Body.String())
		}
	})

	t.Run("Step3_Wrong_Bundle_422", func(t *testing.T) {
		p := basePayload("tx_wrong_bundle")
		p.BundleID = "com.other.app"
		jws := ca.makeJWS(t, p)

		rec := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
			"signed_transaction": jws,
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("got status %d, want 422", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "wrong_bundle") {
			t.Errorf("expected wrong_bundle, got %s", rec.Body.String())
		}
	})

	t.Run("Step3_Unsupported_Product_Type_422", func(t *testing.T) {
		p := basePayload("tx_wrong_type")
		p.Type = "Auto-Renewable Subscription"
		jws := ca.makeJWS(t, p)

		rec := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
			"signed_transaction": jws,
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("got status %d, want 422", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "unsupported_product_type") {
			t.Errorf("expected unsupported_product_type, got %s", rec.Body.String())
		}
	})

	t.Run("Step4_Account_Mismatch_403", func(t *testing.T) {
		p := basePayload("tx_mismatch")
		p.AppAccountToken = models.DeterministicAppAccountToken("other_user_id")
		jws := ca.makeJWS(t, p)

		rec := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
			"signed_transaction": jws,
		})
		if rec.Code != http.StatusForbidden {
			t.Errorf("got status %d, want 403", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "account_mismatch") {
			t.Errorf("expected account_mismatch, got %s", rec.Body.String())
		}
	})

	t.Run("Step6_Revoked_Transaction_422", func(t *testing.T) {
		p := basePayload("tx_revoked_first_time")
		p.RevocationDate = time.Now().UTC().UnixMilli()
		jws := ca.makeJWS(t, p)

		rec := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
			"signed_transaction": jws,
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("got status %d, want 422", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "revoked_transaction") {
			t.Errorf("expected revoked_transaction, got %s", rec.Body.String())
		}
	})

	t.Run("Step7_Sandbox_Not_Allowed_403", func(t *testing.T) {
		unallowedStudent := "usr_unallowed_sandbox"
		unallowedTok := makeStudentToken(t, unallowedStudent)

		p := basePayload("tx_sandbox_unallowed")
		p.AppAccountToken = models.DeterministicAppAccountToken(unallowedStudent)
		jws := ca.makeJWS(t, p)

		rec := postTransaction(t, s, unallowedTok, "test-gateway-secret", map[string]string{
			"signed_transaction": jws,
		})
		if rec.Code != http.StatusForbidden {
			t.Errorf("got status %d, want 403", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "sandbox_not_allowed") {
			t.Errorf("expected sandbox_not_allowed, got %s", rec.Body.String())
		}
	})

	t.Run("Step8_Unknown_Product_404", func(t *testing.T) {
		p := basePayload("tx_unknown_prod")
		p.ProductID = "com.wael.app.pkg.nonexistent"
		jws := ca.makeJWS(t, p)

		rec := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
			"signed_transaction": jws,
		})
		if rec.Code != http.StatusNotFound {
			t.Errorf("got status %d, want 404", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "unknown_product") {
			t.Errorf("expected unknown_product, got %s", rec.Body.String())
		}
	})

	// CRITICAL TEST per user prompt and Decision D3:
	// A processed transaction must stay already_processed even after the sandbox allowlist
	// is emptied and even if the JWS now carries revocationDate.
	t.Run("Step5_ProcessedTransaction_StaysAlreadyProcessed_EvenIfRevokedAndAllowlistEmptied", func(t *testing.T) {
		txID := "tx_idempotent_test_999"
		pInitial := basePayload(txID)
		jwsInitial := ca.makeJWS(t, pInitial)

		// 1. Initial successful purchase -> 200 granted
		rec1 := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
			"signed_transaction": jwsInitial,
		})
		if rec1.Code != http.StatusOK {
			t.Fatalf("first submit: expected 200, got %d (%s)", rec1.Code, rec1.Body.String())
		}
		var resp1 IAPTransactionResponseDTO
		if err := json.Unmarshal(rec1.Body.Bytes(), &resp1); err != nil {
			t.Fatalf("unmarshal resp1: %v", err)
		}
		if resp1.Status != "granted" {
			t.Errorf("resp1.Status = %s, want granted", resp1.Status)
		}

		// 2. Clear the sandbox allowlist entirely!
		s.IAPSandboxAllowedUserIDs = []string{}
		s.IAPSandboxAllowAll = false

		// 3. New JWS with same transactionID, but WITH revocationDate!
		pRevoked := basePayload(txID)
		pRevoked.RevocationDate = time.Now().UTC().UnixMilli()
		jwsRevoked := ca.makeJWS(t, pRevoked)

		// 4. Re-submit by the same user: must STILL return 200 already_processed!
		rec2 := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
			"signed_transaction": jwsRevoked,
		})
		if rec2.Code != http.StatusOK {
			t.Fatalf("second submit: expected 200 already_processed, got %d (%s)", rec2.Code, rec2.Body.String())
		}
		var resp2 IAPTransactionResponseDTO
		if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
			t.Fatalf("unmarshal resp2: %v", err)
		}
		if resp2.Status != "already_processed" {
			t.Errorf("resp2.Status = %s, want already_processed", resp2.Status)
		}
		if resp2.TransactionID != txID {
			t.Errorf("resp2.TransactionID = %s, want %s", resp2.TransactionID, txID)
		}

		// 5. Submit by a DIFFERENT student account -> must return 409 transaction_owned_by_other_user
		otherStudent := "usr_other_student_55"
		otherTok := makeStudentToken(t, otherStudent)
		pOther := basePayload(txID)
		pOther.AppAccountToken = models.DeterministicAppAccountToken(otherStudent)
		jwsOther := ca.makeJWS(t, pOther)

		rec3 := postTransaction(t, s, otherTok, "test-gateway-secret", map[string]string{
			"signed_transaction": jwsOther,
		})
		if rec3.Code != http.StatusConflict {
			t.Errorf("third submit by other user: expected 409, got %d (%s)", rec3.Code, rec3.Body.String())
		}
		if !strings.Contains(rec3.Body.String(), "transaction_owned_by_other_user") {
			t.Errorf("expected transaction_owned_by_other_user, got: %s", rec3.Body.String())
		}
	})
}

func TestIAPTransaction_GrantsAndSkippedReasons(t *testing.T) {
	s := newTestServer(false)
	ca := newIAPTestCA(t)
	s.IAPVerifier = ca.verifier
	s.IAPBundleID = "com.wael.app"
	s.IAPSandboxAllowAll = true

	ctx := context.Background()
	studentID := "usr_stud_skips_1"
	studentTok := makeStudentToken(t, studentID)
	tokenAccount := models.DeterministicAppAccountToken(studentID)

	future := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	past := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)

	// 1. sGrant: normal active subject -> will be granted
	sGrant := createIAPTestSubject(t, s, "subj_grant_ok", "bachelor-y1", models.StatusPublished, future)

	// 2. sOwned: already owned by student -> will be skipped as already_owned
	sOwned := createIAPTestSubject(t, s, "subj_owned_ok", "bachelor-y1", models.StatusPublished, future)
	entOwned := &models.Entitlement{
		UserID:    studentID,
		SubjectID: sOwned.ID,
		Source:    models.EntitlementSourceAdminGrant,
		ExpiresAt: future,
		GrantedAt: time.Now().UTC(),
		Active:    true,
	}
	if err := s.Store.Grant(ctx, entOwned); err != nil {
		t.Fatalf("Grant entOwned: %v", err)
	}

	// 3. sExpired: access_expires_at is in the past -> skipped as expired
	sExpired := createIAPTestSubject(t, s, "subj_expired_ok", "bachelor-y1", models.StatusPublished, past)

	// 4. sDraft: draft status -> skipped as unavailable
	sDraft := createIAPTestSubject(t, s, "subj_draft_ok", "bachelor-y1", models.StatusDraft, future)

	// 5. sHidden: level is unpublished -> skipped as unavailable
	unpubLevel := &models.Level{
		Key:       "diploma-unpub-test",
		StudyType: models.StudyTypeDiploma,
		TitleAr:   "دبلوم غير منشور",
		Published: false,
	}
	if err := s.Store.CreateLevel(ctx, unpubLevel); err != nil {
		t.Fatalf("CreateLevel: %v", err)
	}
	sHidden := createIAPTestSubject(t, s, "subj_hidden_ok", "diploma-unpub-test", models.StatusPublished, future)

	// Package with sGrant, sOwned, sExpired, sDraft, sHidden, and non-existent
	pkg := &models.IAPPackage{
		ID:        "pkg_skips_test",
		ProductID: "com.wael.app.pkg.year1",
		TitleAr:   "باقة تخطي المواد",
		TitleEn:   "Skips Package",
		SubjectIDs: []string{
			sGrant.ID,
			sOwned.ID,
			sExpired.ID,
			sDraft.ID,
			sHidden.ID,
			"subj_nonexistent_99",
		},
		Published: true,
		Position:  1,
	}
	if err := s.Store.CreateIAPPackage(ctx, pkg); err != nil {
		t.Fatalf("CreateIAPPackage: %v", err)
	}

	ms := time.Now().UTC().UnixMilli()
	payload := &iap.JWSTransactionPayload{
		TransactionID:         "tx_skips_test_100",
		OriginalTransactionID: "tx_skips_test_100",
		BundleID:              "com.wael.app",
		ProductID:             pkg.ProductID,
		PurchaseDate:          ms,
		OriginalPurchaseDate:  ms,
		SignedDate:            ms,
		Type:                  "Non-Renewing Subscription",
		Environment:           "Production",
		AppAccountToken:       tokenAccount,
	}
	jws := ca.makeJWS(t, payload)

	rec := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
		"signed_transaction": jws,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var resp IAPTransactionResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Status != "granted" {
		t.Errorf("Status = %s, want granted", resp.Status)
	}
	if len(resp.GrantedSubjectIDs) != 1 || resp.GrantedSubjectIDs[0] != sGrant.ID {
		t.Errorf("GrantedSubjectIDs = %v, want [%s]", resp.GrantedSubjectIDs, sGrant.ID)
	}

	// Verify skipped mapping
	skippedMap := make(map[string]string)
	for _, sk := range resp.Skipped {
		skippedMap[sk.SubjectID] = sk.Reason
	}

	if skippedMap[sOwned.ID] != "already_owned" {
		t.Errorf("sOwned reason = %s, want already_owned", skippedMap[sOwned.ID])
	}
	if skippedMap[sExpired.ID] != "expired" {
		t.Errorf("sExpired reason = %s, want expired", skippedMap[sExpired.ID])
	}
	if skippedMap[sDraft.ID] != "unavailable" {
		t.Errorf("sDraft reason = %s, want unavailable", skippedMap[sDraft.ID])
	}
	if skippedMap[sHidden.ID] != "unavailable" {
		t.Errorf("sHidden reason = %s, want unavailable", skippedMap[sHidden.ID])
	}
	if skippedMap["subj_nonexistent_99"] != "unavailable" {
		t.Errorf("nonexistent reason = %s, want unavailable", skippedMap["subj_nonexistent_99"])
	}
}

func TestIAPTransaction_UnpublishedOrPastAvailableUntil_StillGranted(t *testing.T) {
	// Brief item 7: A valid Apple transaction is always honoured even if package is unpublished
	// or past available_until (Apple charged the student).
	s := newTestServer(false)
	ca := newIAPTestCA(t)
	s.IAPVerifier = ca.verifier
	s.IAPBundleID = "com.wael.app"
	s.IAPSandboxAllowAll = true

	ctx := context.Background()
	studentID := "usr_stud_unpub_grant"
	studentTok := makeStudentToken(t, studentID)
	tokenAccount := models.DeterministicAppAccountToken(studentID)

	future := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	past := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	s1 := createIAPTestSubject(t, s, "subj_unpub_1", "bachelor-y1", models.StatusPublished, future)

	pkg := &models.IAPPackage{
		ID:             "pkg_unpub_expired",
		ProductID:      "com.wael.app.pkg.year3",
		TitleAr:        "باقة غير منشورة ومنتهية البيع",
		TitleEn:        "Unpublished Expired Package",
		SubjectIDs:     []string{s1.ID},
		AvailableUntil: &past,
		Published:      false,
		Position:       5,
	}
	if err := s.Store.CreateIAPPackage(ctx, pkg); err != nil {
		t.Fatalf("CreateIAPPackage: %v", err)
	}

	ms := time.Now().UTC().UnixMilli()
	payload := &iap.JWSTransactionPayload{
		TransactionID:         "tx_unpub_honoured_1",
		OriginalTransactionID: "tx_unpub_honoured_1",
		BundleID:              "com.wael.app",
		ProductID:             pkg.ProductID,
		PurchaseDate:          ms,
		OriginalPurchaseDate:  ms,
		SignedDate:            ms,
		Type:                  "Non-Renewing Subscription",
		Environment:           "Production",
		AppAccountToken:       tokenAccount,
	}
	jws := ca.makeJWS(t, payload)

	rec := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
		"signed_transaction": jws,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var resp IAPTransactionResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "granted" {
		t.Errorf("status = %s, want granted", resp.Status)
	}
	if len(resp.GrantedSubjectIDs) != 1 || resp.GrantedSubjectIDs[0] != s1.ID {
		t.Errorf("GrantedSubjectIDs = %v, want [%s]", resp.GrantedSubjectIDs, s1.ID)
	}
}

func TestIAPTransaction_Concurrency_Race(t *testing.T) {
	s := newTestServer(false)
	ca := newIAPTestCA(t)
	s.IAPVerifier = ca.verifier
	s.IAPBundleID = "com.wael.app"
	s.IAPSandboxAllowAll = true

	ctx := context.Background()
	studentID := "usr_stud_concurrency"
	studentTok := makeStudentToken(t, studentID)
	tokenAccount := models.DeterministicAppAccountToken(studentID)

	future := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	s1 := createIAPTestSubject(t, s, "subj_conc_1", "bachelor-y1", models.StatusPublished, future)

	pkg := &models.IAPPackage{
		ID:         "pkg_conc_test",
		ProductID:  "com.wael.app.pkg.year2",
		TitleAr:    "باقة فحص التزامن",
		TitleEn:    "Concurrency Package",
		SubjectIDs: []string{s1.ID},
		Published:  true,
		Position:   2,
	}
	if err := s.Store.CreateIAPPackage(ctx, pkg); err != nil {
		t.Fatalf("CreateIAPPackage: %v", err)
	}

	ms := time.Now().UTC().UnixMilli()
	payload := &iap.JWSTransactionPayload{
		TransactionID:         "tx_conc_test_race_1",
		OriginalTransactionID: "tx_conc_test_race_1",
		BundleID:              "com.wael.app",
		ProductID:             pkg.ProductID,
		PurchaseDate:          ms,
		OriginalPurchaseDate:  ms,
		SignedDate:            ms,
		Type:                  "Non-Renewing Subscription",
		Environment:           "Production",
		AppAccountToken:       tokenAccount,
	}
	jws := ca.makeJWS(t, payload)

	const concurrency = 10
	var wg sync.WaitGroup
	wg.Add(concurrency)

	results := make([]int, concurrency)
	statuses := make([]string, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			rec := postTransaction(t, s, studentTok, "test-gateway-secret", map[string]string{
				"signed_transaction": jws,
			})
			results[idx] = rec.Code
			var resp IAPTransactionResponseDTO
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			statuses[idx] = resp.Status
		}(i)
	}
	wg.Wait()

	grantedCount := 0
	alreadyProcessedCount := 0
	for i := 0; i < concurrency; i++ {
		if results[i] != http.StatusOK {
			t.Errorf("goroutine %d failed with code %d", i, results[i])
		}
		if statuses[i] == "granted" {
			grantedCount++
		} else if statuses[i] == "already_processed" {
			alreadyProcessedCount++
		}
	}

	if grantedCount != 1 {
		t.Errorf("expected exactly 1 'granted', got %d (already_processed: %d)", grantedCount, alreadyProcessedCount)
	}
	if grantedCount+alreadyProcessedCount != concurrency {
		t.Errorf("expected total %d successful answers, got %d", concurrency, grantedCount+alreadyProcessedCount)
	}
}
