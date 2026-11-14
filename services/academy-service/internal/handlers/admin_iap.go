package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

var iapProductIDPattern = regexp.MustCompile(`^com\.wael\.app\.pkg\.[a-z0-9_.]+$`)

// IAPPackageAdminDTO is the admin view of an IAP package.
type IAPPackageAdminDTO struct {
	ID             string     `json:"id"`
	ProductID      string     `json:"product_id"`
	TitleAr        string     `json:"title_ar"`
	TitleEn        string     `json:"title_en"`
	DescriptionAr  string     `json:"description_ar"`
	DescriptionEn  string     `json:"description_en"`
	SubjectIDs     []string   `json:"subject_ids"`
	AvailableUntil *time.Time `json:"available_until"`
	Published      bool       `json:"published"`
	Position       int        `json:"position"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type createIAPPackageRequest struct {
	ProductID      string     `json:"product_id"`
	TitleAr        string     `json:"title_ar"`
	TitleEn        string     `json:"title_en"`
	DescriptionAr  *string    `json:"description_ar"`
	DescriptionEn  *string    `json:"description_en"`
	SubjectIDs     []string   `json:"subject_ids"`
	AvailableUntil *time.Time `json:"available_until"`
	Published      *bool      `json:"published"`
	Position       *int       `json:"position"`
}

// NullableTime handles unmarshaling of optional nullable timestamps in PATCH bodies.
type NullableTime struct {
	Valid bool
	Time  *time.Time
}

func (n *NullableTime) UnmarshalJSON(b []byte) error {
	n.Valid = true
	if string(b) == "null" {
		n.Time = nil
		return nil
	}
	var t time.Time
	if err := json.Unmarshal(b, &t); err != nil {
		return err
	}
	n.Time = &t
	return nil
}

type patchIAPPackageRequest struct {
	ProductID      *string      `json:"product_id"`
	TitleAr        *string      `json:"title_ar"`
	TitleEn        *string      `json:"title_en"`
	DescriptionAr  *string      `json:"description_ar"`
	DescriptionEn  *string      `json:"description_en"`
	SubjectIDs     *[]string    `json:"subject_ids"`
	AvailableUntil NullableTime `json:"available_until"`
	Published      *bool        `json:"published"`
	Position       *int         `json:"position"`
}

func cleanIAPTitle(raw string) (string, bool) {
	s := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(raw), "\r", ""), "\n", "")
	if s == "" {
		return "", false
	}
	if utf8.RuneCountInString(s) > 80 {
		return "", false
	}
	return s, true
}

func cleanIAPDescription(raw string) (string, bool) {
	s := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(raw), "\r", " "), "\n", " ")
	if utf8.RuneCountInString(s) > 1000 {
		return "", false
	}
	return s, true
}

// existingSubjectIDs returns the package's stored subject IDs that still
// exist, in package order. Draft, expired and hidden-level subjects are kept:
// the admin console edits the package from this list and saves it back, so
// dropping them here would remove them from the package on the next save
// (owner fix 2026-11-14, W5). Only deleted subjects are omitted (a PATCH that
// sends them back would fail with unknown_subject). Student reads (#54) and
// coverage still filter to active subjects.
func existingSubjectIDs(ctx context.Context, st store.Store, subjectIDs []string, subjectCache map[string]*models.Subject) ([]string, error) {
	if len(subjectIDs) == 0 {
		return []string{}, nil
	}
	if subjectCache == nil {
		subjectCache = make(map[string]*models.Subject)
	}
	kept := make([]string, 0, len(subjectIDs))
	for _, id := range subjectIDs {
		subj, ok := subjectCache[id]
		if !ok {
			var err error
			subj, err = st.GetSubjectByID(ctx, id)
			if err != nil {
				return nil, err
			}
			subjectCache[id] = subj
		}
		if subj == nil {
			continue // deleted or missing
		}
		kept = append(kept, id)
	}
	return kept, nil
}

func toIAPPackageAdminDTO(ctx context.Context, st store.Store, pkg *models.IAPPackage, subjectCache map[string]*models.Subject) (IAPPackageAdminDTO, error) {
	subjectIDs, err := existingSubjectIDs(ctx, st, pkg.SubjectIDs, subjectCache)
	if err != nil {
		return IAPPackageAdminDTO{}, err
	}
	return IAPPackageAdminDTO{
		ID:             pkg.ID,
		ProductID:      pkg.ProductID,
		TitleAr:        pkg.TitleAr,
		TitleEn:        pkg.TitleEn,
		DescriptionAr:  pkg.DescriptionAr,
		DescriptionEn:  pkg.DescriptionEn,
		SubjectIDs:     subjectIDs,
		AvailableUntil: pkg.AvailableUntil,
		Published:      pkg.Published,
		Position:       pkg.Position,
		CreatedAt:      pkg.CreatedAt,
		UpdatedAt:      pkg.UpdatedAt,
	}, nil
}

// AdminIAPPackages dispatches GET (list) and POST (create) on /internal/admin/iap/packages.
func (s *Server) AdminIAPPackages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.ListAdminIAPPackages(w, r)
	case http.MethodPost:
		s.CreateAdminIAPPackage(w, r)
	default:
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

// ListAdminIAPPackages handles GET /internal/admin/iap/packages.
func (s *Server) ListAdminIAPPackages(w http.ResponseWriter, r *http.Request) {
	if _, ok := AdminFromRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	packages, err := s.Store.ListIAPPackages(dbCtx, false)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	subjectCache := make(map[string]*models.Subject)

	dtos := make([]IAPPackageAdminDTO, 0, len(packages))
	for _, pkg := range packages {
		dto, err := toIAPPackageAdminDTO(dbCtx, s.Store, pkg, subjectCache)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		dtos = append(dtos, dto)
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"packages": dtos})
}

// CreateAdminIAPPackage handles POST /internal/admin/iap/packages.
func (s *Server) CreateAdminIAPPackage(w http.ResponseWriter, r *http.Request) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	var req createIAPPackageRequest
	if !decodeAdminJSON(w, r, &req) {
		return
	}

	req.ProductID = strings.TrimSpace(req.ProductID)
	if !iapProductIDPattern.MatchString(req.ProductID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_product_id", "product_id must match ^com\\.wael\\.app\\.pkg\\.[a-z0-9_.]+$", nil)
		return
	}

	titleAr, ok := cleanIAPTitle(req.TitleAr)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_title", "title_ar must be 1-80 characters", nil)
		return
	}

	titleEn, ok := cleanIAPTitle(req.TitleEn)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_title", "title_en must be 1-80 characters", nil)
		return
	}

	descAr := ""
	if req.DescriptionAr != nil {
		v, ok := cleanIAPDescription(*req.DescriptionAr)
		if !ok {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_description", "description_ar must be at most 1000 characters", nil)
			return
		}
		descAr = v
	}

	descEn := ""
	if req.DescriptionEn != nil {
		v, ok := cleanIAPDescription(*req.DescriptionEn)
		if !ok {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_description", "description_en must be at most 1000 characters", nil)
			return
		}
		descEn = v
	}

	if len(req.SubjectIDs) > 100 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_subject_ids", "subject_ids cannot exceed 100 items", nil)
		return
	}

	seenSubj := make(map[string]bool, len(req.SubjectIDs))
	for _, sid := range req.SubjectIDs {
		if !validResourceID(sid) || seenSubj[sid] {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_subject_ids", "duplicate or invalid subject id", nil)
			return
		}
		seenSubj[sid] = true
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	for _, sid := range req.SubjectIDs {
		sObj, err := s.Store.GetSubjectByID(dbCtx, sid)
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		if sObj == nil {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "unknown_subject", fmt.Sprintf("subject not found: %s", sid), nil)
			return
		}
	}

	pos := 0
	if req.Position != nil {
		pos = *req.Position
	}
	pub := false
	if req.Published != nil {
		pub = *req.Published
	}

	id, err := jwtutil.GenerateUUID()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "failed to generate package id", err)
		return
	}
	pkgID := "pkg_" + strings.ReplaceAll(id, "-", "")[:16]

	now := time.Now().UTC()
	subjectIDs := req.SubjectIDs
	if subjectIDs == nil {
		subjectIDs = []string{}
	}

	pkg := &models.IAPPackage{
		ID:             pkgID,
		ProductID:      req.ProductID,
		TitleAr:        titleAr,
		TitleEn:        titleEn,
		DescriptionAr:  descAr,
		DescriptionEn:  descEn,
		SubjectIDs:     subjectIDs,
		AvailableUntil: req.AvailableUntil,
		Published:      pub,
		Position:       pos,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.Store.CreateIAPPackage(dbCtx, pkg); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			handlerutil.WriteSafeError(w, r, http.StatusConflict, "duplicate_product_id", "package with this product_id already exists", nil)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if err := s.writeAdminAudit(r.Context(), adm, "iap_package_create", "iap_package", pkg.ID, pkg.ProductID); err != nil {
		logAuditFailure(adm, "iap_package_create", pkg.ID, err)
	}

	dto, err := toIAPPackageAdminDTO(dbCtx, s.Store, pkg, nil)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	handlerutil.WriteJSON(w, http.StatusCreated, dto)
}

// AdminIAPPackageSubroute dispatches GET, PATCH, and DELETE on /internal/admin/iap/packages/{id}.
func (s *Server) AdminIAPPackageSubroute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/internal/admin/iap/packages/")
	rest = strings.Trim(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) != 1 || parts[0] == "" || !validResourceID(parts[0]) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_package_id", "invalid package id", nil)
		return
	}
	id := parts[0]

	switch r.Method {
	case http.MethodGet:
		s.GetAdminIAPPackage(w, r, id)
	case http.MethodPatch:
		s.PatchAdminIAPPackage(w, r, id)
	case http.MethodDelete:
		s.DeleteAdminIAPPackage(w, r, id)
	default:
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

// GetAdminIAPPackage handles GET /internal/admin/iap/packages/{id}.
func (s *Server) GetAdminIAPPackage(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := AdminFromRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	pkg, err := s.Store.GetIAPPackage(dbCtx, id)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if pkg == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "package not found", nil)
		return
	}

	dto, err := toIAPPackageAdminDTO(dbCtx, s.Store, pkg, nil)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, dto)
}

// PatchAdminIAPPackage handles PATCH /internal/admin/iap/packages/{id}.
func (s *Server) PatchAdminIAPPackage(w http.ResponseWriter, r *http.Request, id string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	var req patchIAPPackageRequest
	if !decodeAdminJSON(w, r, &req) {
		return
	}

	// product_id is immutable: sending product_id in request body returns 400 product_id_immutable.
	if req.ProductID != nil {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "product_id_immutable", "product_id is immutable", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	pkg, err := s.Store.GetIAPPackage(dbCtx, id)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if pkg == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "package not found", nil)
		return
	}

	if req.TitleAr != nil {
		titleAr, ok := cleanIAPTitle(*req.TitleAr)
		if !ok {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_title", "title_ar must be 1-80 characters", nil)
			return
		}
		pkg.TitleAr = titleAr
	}

	if req.TitleEn != nil {
		titleEn, ok := cleanIAPTitle(*req.TitleEn)
		if !ok {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_title", "title_en must be 1-80 characters", nil)
			return
		}
		pkg.TitleEn = titleEn
	}

	if req.DescriptionAr != nil {
		descAr, ok := cleanIAPDescription(*req.DescriptionAr)
		if !ok {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_description", "description_ar must be at most 1000 characters", nil)
			return
		}
		pkg.DescriptionAr = descAr
	}

	if req.DescriptionEn != nil {
		descEn, ok := cleanIAPDescription(*req.DescriptionEn)
		if !ok {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_description", "description_en must be at most 1000 characters", nil)
			return
		}
		pkg.DescriptionEn = descEn
	}

	if req.SubjectIDs != nil {
		if len(*req.SubjectIDs) > 100 {
			handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_subject_ids", "subject_ids cannot exceed 100 items", nil)
			return
		}
		seenSubj := make(map[string]bool, len(*req.SubjectIDs))
		for _, sid := range *req.SubjectIDs {
			if !validResourceID(sid) || seenSubj[sid] {
				handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_subject_ids", "duplicate or invalid subject id", nil)
				return
			}
			seenSubj[sid] = true
		}
		for _, sid := range *req.SubjectIDs {
			sObj, err := s.Store.GetSubjectByID(dbCtx, sid)
			if err != nil {
				handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
				return
			}
			if sObj == nil {
				handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "unknown_subject", fmt.Sprintf("subject not found: %s", sid), nil)
				return
			}
		}
		pkg.SubjectIDs = *req.SubjectIDs
	}

	if req.AvailableUntil.Valid {
		pkg.AvailableUntil = req.AvailableUntil.Time
	}

	if req.Published != nil {
		pkg.Published = *req.Published
	}

	if req.Position != nil {
		pkg.Position = *req.Position
	}

	pkg.UpdatedAt = time.Now().UTC()

	if err := s.Store.UpdateIAPPackage(dbCtx, pkg); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "package not found", nil)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	if err := s.writeAdminAudit(r.Context(), adm, "iap_package_update", "iap_package", pkg.ID, pkg.ProductID); err != nil {
		logAuditFailure(adm, "iap_package_update", pkg.ID, err)
	}

	dto, err := toIAPPackageAdminDTO(dbCtx, s.Store, pkg, nil)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, dto)
}

// DeleteAdminIAPPackage handles DELETE /internal/admin/iap/packages/{id}.
func (s *Server) DeleteAdminIAPPackage(w http.ResponseWriter, r *http.Request, id string) {
	adm, ok := AdminFromRequest(r)
	if !ok || adm == nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	pkg, err := s.Store.GetIAPPackage(dbCtx, id)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if pkg == nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "package not found", nil)
		return
	}

	count, err := s.Store.CountIAPPurchasesByPackage(dbCtx, id)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if count > 0 {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "package_has_purchases", "cannot delete package with existing purchases", nil)
		return
	}

	if err := s.Store.DeleteIAPPackage(dbCtx, id); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "package not found", nil)
			return
		case errors.Is(err, store.ErrConflict):
			handlerutil.WriteSafeError(w, r, http.StatusConflict, "package_has_purchases", "cannot delete package with existing purchases", nil)
			return
		default:
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
	}

	if err := s.writeAdminAudit(r.Context(), adm, "iap_package_delete", "iap_package", id, pkg.ProductID); err != nil {
		logAuditFailure(adm, "iap_package_delete", id, err)
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AdminIAPPurchases handles GET /internal/admin/iap/purchases?user_id=&package_id=&limit=.
func (s *Server) AdminIAPPurchases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	if _, ok := AdminFromRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	q := r.URL.Query()
	userID := strings.TrimSpace(q.Get("user_id"))
	if userID != "" && !validResourceID(userID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_user_id", "invalid user id", nil)
		return
	}

	packageID := strings.TrimSpace(q.Get("package_id"))
	if packageID != "" && !validResourceID(packageID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_package_id", "invalid package id", nil)
		return
	}

	limit := 50
	if lStr := strings.TrimSpace(q.Get("limit")); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil {
			if l <= 0 {
				limit = 50
			} else if l > 100 {
				limit = 100
			} else {
				limit = l
			}
		}
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	purchases, err := s.Store.ListIAPPurchases(dbCtx, models.IAPPurchaseFilter{
		UserID:    userID,
		PackageID: packageID,
	}, limit)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if purchases == nil {
		purchases = []*models.IAPPurchase{}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"purchases": purchases})
}

// AdminIAPCoverage handles GET /internal/admin/iap/coverage.
// Reports published subjects under published levels, not expired, that are in no published package.
func (s *Server) AdminIAPCoverage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	if _, ok := AdminFromRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	allLevels, err := s.Store.ListLevels(dbCtx, false)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	levelsByKey := make(map[string]*models.Level, len(allLevels))
	for _, l := range allLevels {
		levelsByKey[l.Key] = l
	}

	// Fetch all published subjects across pages
	var allPublishedSubjects []*models.Subject
	for page := 1; ; page++ {
		batch, total, err := s.Store.ListSubjects(dbCtx, store.SubjectFilter{
			Status: models.StatusPublished,
			Page:   page,
			Limit:  100,
		})
		if err != nil {
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
			return
		}
		allPublishedSubjects = append(allPublishedSubjects, batch...)
		if len(batch) < 100 || len(allPublishedSubjects) >= total {
			break
		}
	}

	now := time.Now().UTC()
	var candidates []*models.Subject
	for _, subj := range allPublishedSubjects {
		if subj.Status != models.StatusPublished {
			continue
		}
		lvl := levelsByKey[subj.LevelKey]
		if lvl == nil || !lvl.Published {
			continue
		}
		if !subj.AccessExpiresAt.IsZero() && !subj.AccessExpiresAt.After(now) {
			continue
		}
		candidates = append(candidates, subj)
	}

	// Fetch all published packages to determine coverage
	pubPackages, err := s.Store.ListIAPPackages(dbCtx, true)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}

	covered := make(map[string]bool)
	for _, pkg := range pubPackages {
		for _, sid := range pkg.SubjectIDs {
			covered[sid] = true
		}
	}

	uncovered := make([]string, 0)
	for _, cand := range candidates {
		if !covered[cand.ID] {
			uncovered = append(uncovered, cand.ID)
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"uncovered_subject_ids": uncovered,
	})
}
