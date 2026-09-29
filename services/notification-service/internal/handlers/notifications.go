// Package handlers implements the notification-service HTTP API:
// JWT-authenticated SSE stream, paginated list, mark-read, and an internal
// push endpoint guarded by INTERNAL_SERVICE_TOKEN. Query strings (notably
// ?token=) are never written to logs; only method and path are logged.
package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/bus"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/models"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

// Server wires notification dependencies.
type Server struct {
	Store         store.Store
	Bus           bus.Bus
	GatewaySecret string
	InternalToken string
}

// New creates a Server.
func New(st store.Store, b bus.Bus, gatewaySecret, internalToken string) *Server {
	return &Server{Store: st, Bus: b, GatewaySecret: gatewaySecret, InternalToken: internalToken}
}

// GatewayAuth requires the gateway secret on user routes (not /health;
// /internal/* carries its own INTERNAL_SERVICE_TOKEN auth instead).
func (s *Server) GatewayAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/internal/") {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-Gateway-Secret")
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.GatewaySecret)) != 1 {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// InternalAuth requires INTERNAL_SERVICE_TOKEN on /internal/* routes.
func (s *Server) InternalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Internal-Token")
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.InternalToken)) != 1 {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bearerOrQuery extracts the JWT from Authorization header, falling back to
// ?token= for SSE clients that cannot set headers. Callers must never log
// the query string.
func bearerOrQuery(r *http.Request) string {
	if t := handlerutil.BearerToken(r); t != "" {
		return t
	}
	return r.URL.Query().Get("token")
}

func (s *Server) authenticate(r *http.Request) (*jwtutil.Claims, error) {
	token := bearerOrQuery(r)
	if token == "" {
		return nil, fmt.Errorf("missing token")
	}
	return jwtutil.ValidateToken(token)
}

// Stream serves GET /notifications/stream as server-sent events for the
// JWT-authenticated user, replaying nothing and pushing live items.
func (s *Server) Stream(w http.ResponseWriter, r *http.Request) {
	claims, err := s.authenticate(r)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "stream unsupported", nil)
		return
	}
	ch, unsub, err := s.Bus.Subscribe(r.Context(), claims.UserID)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	defer unsub()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case n, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(n)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// List serves GET /notifications/list?page=&limit= for the JWT user.
func (s *Server) List(w http.ResponseWriter, r *http.Request) {
	claims, err := s.authenticate(r)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	items, err := s.Store.List(r.Context(), claims.UserID, page, limit)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"notifications": items, "page": pageOrDefault(page)})
}

type readRequest struct {
	ID string `json:"id"`
}

// MarkRead serves POST /notifications/read, scoping the item to the JWT user.
func (s *Server) MarkRead(w http.ResponseWriter, r *http.Request) {
	claims, err := s.authenticate(r)
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	var req readRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || strings.TrimSpace(req.ID) == "" {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid request body", err)
		return
	}
	if err := s.Store.MarkRead(r.Context(), claims.UserID, req.ID); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, handlerutil.ErrCodeInvalidToken, "notification not found", nil)
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type pushRequest struct {
	UserID      string `json:"user_id"`
	Title       string `json:"title"`
	TitleAr     string `json:"title_ar"`
	Body        string `json:"body"`
	BodyAr      string `json:"body_ar"`
	Type        string `json:"type"`
	TargetRoute string `json:"target_route"`
}

// Push serves POST /internal/push for trusted services: persists then fans out.
func (s *Server) Push(w http.ResponseWriter, r *http.Request) {
	var req pushRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.Title) == "" {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid request body", err)
		return
	}
	id, err := jwtutil.GenerateUUID()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	ntype := strings.TrimSpace(req.Type)
	if ntype == "" {
		ntype = "system"
	}
	n := &models.Notification{
		ID: id, UserID: req.UserID, Title: req.Title, TitleAr: req.TitleAr,
		Body: req.Body, BodyAr: req.BodyAr, Type: ntype, TargetRoute: req.TargetRoute,
	}
	if err := s.Store.Create(r.Context(), n); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	if err := s.Bus.Publish(r.Context(), n); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusInternalServerError, handlerutil.ErrCodeInternal, "request failed", err)
		return
	}
	handlerutil.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// Health is the unauthenticated liveness probe.
func Health(w http.ResponseWriter, _ *http.Request) {
	handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func pageOrDefault(page int) int {
	if page < 1 {
		return 1
	}
	return page
}
