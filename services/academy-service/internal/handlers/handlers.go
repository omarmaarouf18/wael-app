// Package handlers implements HTTP request handling for academy-service.
package handlers

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

type contextKey string

const (
	studentUserKey contextKey = "student_user"
)

// Server holds dependencies for HTTP handlers.
type Server struct {
	store          store.Store
	appEnv         string
	gatewaySecret  string
	internalToken  string
	authServiceURL string
}

// New creates a new Server instance.
func New(store store.Store, appEnv, gatewaySecret, internalToken, authServiceURL string) *Server {
	return &Server{
		store:          store,
		appEnv:         appEnv,
		gatewaySecret:  gatewaySecret,
		internalToken:  internalToken,
		authServiceURL: authServiceURL,
	}
}

// GatewayAuth middleware rejects requests lacking the correct X-Gateway-Secret,
// except /health which is permitted directly for stack healthchecks.
func (s *Server) GatewayAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		secret := r.Header.Get("X-Gateway-Secret")
		if secret == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(s.gatewaySecret)) != 1 {
			handlerutil.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// InternalTokenAuth middleware verifies the internal service token on the admin listener.
func (s *Server) InternalTokenAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Internal-Token")
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.internalToken)) != 1 {
			handlerutil.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// StudentAuth middleware validates a student Bearer JWT on every request
// (including Redis revocation marker and denylist checks via jwtutil.ValidateToken).
func (s *Server) StudentAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHdr := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHdr, "Bearer ") {
			handlerutil.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or malformed authorization header"})
			return
		}
		tokenStr := strings.TrimPrefix(authHdr, "Bearer ")
		claims, err := jwtutil.ValidateToken(tokenStr)
		if err != nil {
			handlerutil.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired token"})
			return
		}
		ctx := context.WithValue(r.Context(), studentUserKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// Health reports service liveness.
func Health(w http.ResponseWriter, _ *http.Request) {
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetLevels serves GET /academy/levels: returns the tree of academic study types and levels.
// Per SPEC Section 1 Decision 2 amendment, levels with no published subjects are hidden.
func (s *Server) GetLevels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	ctx := r.Context()
	levels, err := s.store.ListLevels(ctx, true)
	if err != nil {
		handlerutil.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	levelDTOs := make([]models.LevelDTO, 0, len(levels))
	for _, l := range levels {
		levelDTOs = append(levelDTOs, l.ToDTO())
	}

	// Group by study type for catalog tree navigation
	typeMap := make(map[string][]models.LevelDTO)
	for _, l := range levelDTOs {
		typeMap[l.StudyType] = append(typeMap[l.StudyType], l)
	}

	studyTypes := make([]models.StudyTypeDTO, 0)
	typeDefs := []struct {
		Key string
		Ar  string
		En  string
	}{
		{models.StudyTypeBachelor, "الليسانس", "Bachelor"},
		{models.StudyTypeDiploma, "الدبلومات", "Diplomas"},
		{models.StudyTypeVocational, "التدريب المهني", "Vocational Training"},
	}

	for _, td := range typeDefs {
		if lvls, ok := typeMap[td.Key]; ok && len(lvls) > 0 {
			studyTypes = append(studyTypes, models.StudyTypeDTO{
				Key: td.Key,
				Title: models.LocalizedText{
					Ar: td.Ar,
					En: td.En,
				},
				Levels: lvls,
			})
		}
	}

	resp := models.LevelsResponseDTO{
		Levels:     levelDTOs,
		StudyTypes: studyTypes,
	}

	handlerutil.WriteJSON(w, http.StatusOK, resp)
}

// AdminHandler returns an http.Handler for the internal admin listener.
// In Phase 2, the admin surface is not yet implemented, returning 404 for all paths.
func (s *Server) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/admin/", func(w http.ResponseWriter, r *http.Request) {
		handlerutil.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	var handler http.Handler = mux
	handler = s.InternalTokenAuth(handler)
	handler = handlerutil.MaxBytesMiddleware(1 << 20)(handler)
	return handler
}

// PublicHandler returns an http.Handler for the public listener routing student requests.
func (s *Server) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", Health)
	mux.HandleFunc("/academy/levels", s.StudentAuth(s.GetLevels))

	var handler http.Handler = mux
	handler = s.GatewayAuth(handler)
	handler = handlerutil.MaxBytesMiddleware(1 << 20)(handler)
	return handler
}
