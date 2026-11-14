package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
)

// IAPSubjectDTO represents a single subject within an IAP package as seen by a student.
// It carries NO price, NO currency, and NO video or content storage identifiers.
type IAPSubjectDTO struct {
	ID       string               `json:"id"`
	Title    models.LocalizedText `json:"title"`
	LevelKey string               `json:"level_key"`
	Term     string               `json:"term"`
	Owned    bool                 `json:"owned"`
}

// IAPPackageStudentDTO represents an IAP package returned to student clients.
// It carries NO price, NO currency, and NO video or content storage identifiers.
type IAPPackageStudentDTO struct {
	ID             string               `json:"id"`
	ProductID      string               `json:"product_id"`
	Title          models.LocalizedText `json:"title"`
	Description    models.LocalizedText `json:"description"`
	AvailableUntil *time.Time           `json:"available_until"`
	OnSale         bool                 `json:"on_sale"`
	Position       int                  `json:"position"`
	Subjects       []IAPSubjectDTO      `json:"subjects"`
}

// IAPPackagesResponseDTO represents the full response body of GET /academy/iap/packages.
type IAPPackagesResponseDTO struct {
	AppAccountToken string                 `json:"app_account_token"`
	Packages        []IAPPackageStudentDTO `json:"packages"`
}

// GetIAPPackages handles GET /academy/iap/packages (student TierRead).
// Enforces student authentication and forbids web tokens (claims.Client == "web") with 403 app_client_required.
// Skips any subject ID that no longer exists, is draft, is under an unpublished level, or is expired.
func (s *Server) GetIAPPackages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	claims := StudentClaims(r)
	if claims == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	if claims.Client == "web" {
		handlerutil.WriteSafeError(w, r, http.StatusForbidden, "app_client_required", "mobile app client required", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	// Only published packages, ordered by position asc, then title_ar asc
	packages, err := s.Store.ListIAPPackages(dbCtx, true)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	ownedMap, err := s.Store.GetActiveEntitlementSubjectIDs(dbCtx, claims.UserID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if ownedMap == nil {
		ownedMap = make(map[string]bool)
	}

	allLevels, err := s.Store.ListLevels(dbCtx, false)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	levelsByKey := make(map[string]*models.Level, len(allLevels))
	for _, l := range allLevels {
		levelsByKey[l.Key] = l
	}

	now := time.Now().UTC()
	subjectCache := make(map[string]*models.Subject)

	pkgDTOs := make([]IAPPackageStudentDTO, 0, len(packages))
	for _, pkg := range packages {
		subjs := make([]IAPSubjectDTO, 0, len(pkg.SubjectIDs))
		for _, sid := range pkg.SubjectIDs {
			subj, ok := subjectCache[sid]
			if !ok {
				var err error
				subj, err = s.Store.GetSubjectByID(dbCtx, sid)
				if err != nil {
					handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
					return
				}
				subjectCache[sid] = subj
			}
			if subj == nil {
				continue // missing or deleted
			}
			if subj.Status != models.StatusPublished {
				continue // draft
			}
			lvl := levelsByKey[subj.LevelKey]
			if lvl == nil || !lvl.Published {
				continue // unpublished or missing level
			}
			if !subj.AccessExpiresAt.IsZero() && !subj.AccessExpiresAt.After(now) {
				continue // expired
			}
			subjs = append(subjs, IAPSubjectDTO{
				ID: sid,
				Title: models.LocalizedText{
					Ar: subj.TitleAr,
					En: subj.TitleEn,
				},
				LevelKey: subj.LevelKey,
				Term:     subj.Term,
				Owned:    ownedMap[sid],
			})
		}

		onSale := (pkg.AvailableUntil == nil || now.Before(*pkg.AvailableUntil)) && len(subjs) > 0

		pkgDTOs = append(pkgDTOs, IAPPackageStudentDTO{
			ID:        pkg.ID,
			ProductID: pkg.ProductID,
			Title: models.LocalizedText{
				Ar: pkg.TitleAr,
				En: pkg.TitleEn,
			},
			Description: models.LocalizedText{
				Ar: pkg.DescriptionAr,
				En: pkg.DescriptionEn,
			},
			AvailableUntil: pkg.AvailableUntil,
			OnSale:         onSale,
			Position:       pkg.Position,
			Subjects:       subjs,
		})
	}

	resp := IAPPackagesResponseDTO{
		AppAccountToken: models.DeterministicAppAccountToken(claims.UserID),
		Packages:        pkgDTOs,
	}

	handlerutil.WriteJSON(w, http.StatusOK, resp)
}
