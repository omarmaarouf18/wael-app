// Package handlers implements the notification-service HTTP API:
// JWT-authenticated SSE stream, paginated list, mark-read, and an internal
// push endpoint guarded by INTERNAL_SERVICE_TOKEN. Query strings (notably
// ?token=) are never written to logs; only method and path are logged.
package handlers

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/bus"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/models"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

// streamSlot tracks an individual active stream for a user.
type streamSlot struct {
	id     uint64
	userID string
	sid    string
	cancel context.CancelFunc
	once   sync.Once
}

// StreamLimiter manages per-account concurrent stream slots, connection open rate limits,
// and in-process stream registry by user and session for revocation.
type StreamLimiter struct {
	mu            sync.Mutex
	maxConcurrent int
	rateLimit     int
	window        time.Duration
	nextID        uint64
	userStreams   map[string][]*streamSlot
	sidStreams    map[string][]*streamSlot
	openAttempts  map[string][]time.Time
}

// NewStreamLimiter creates a StreamLimiter with max concurrent connections and open rate limit per window.
func NewStreamLimiter(maxConcurrent, rateLimit int, window time.Duration) *StreamLimiter {
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}
	if rateLimit <= 0 {
		rateLimit = 10
	}
	if window <= 0 {
		window = time.Minute
	}
	return &StreamLimiter{
		maxConcurrent: maxConcurrent,
		rateLimit:     rateLimit,
		window:        window,
		userStreams:   make(map[string][]*streamSlot),
		sidStreams:    make(map[string][]*streamSlot),
		openAttempts:  make(map[string][]time.Time),
	}
}

// acquireStreamSlot attempts to reserve a concurrent stream slot for userID and sid.
// Under the "newest wins" policy, if the user has reached maxConcurrent, the oldest
// active stream(s) are evicted by cancelling their context(s) so their handlers return
// and release their slots. The new stream is always accepted.
// Eviction and release are idempotent (sync.Once).
func (sl *StreamLimiter) acquireStreamSlot(parent context.Context, userID, sid string) (context.Context, func()) {
	streamCtx, cancel := context.WithCancel(parent)

	sl.mu.Lock()
	sl.nextID++
	slot := &streamSlot{
		id:     sl.nextID,
		userID: userID,
		sid:    sid,
		cancel: cancel,
	}

	var toEvict []*streamSlot
	for len(sl.userStreams[userID]) >= sl.maxConcurrent {
		oldest := sl.userStreams[userID][0]
		sl.userStreams[userID] = sl.userStreams[userID][1:]
		if oldest.sid != "" {
			sl.removeSidSlotLocked(oldest.sid, oldest.id)
		}
		toEvict = append(toEvict, oldest)
	}

	sl.userStreams[userID] = append(sl.userStreams[userID], slot)
	if sid != "" {
		sl.sidStreams[sid] = append(sl.sidStreams[sid], slot)
	}
	sl.mu.Unlock()

	// Cancel evicted streams outside lock
	for _, e := range toEvict {
		e.once.Do(func() {
			e.cancel()
		})
	}

	release := func() {
		slot.once.Do(func() {
			slot.cancel()
			sl.mu.Lock()
			defer sl.mu.Unlock()
			streams := sl.userStreams[userID]
			for i, s := range streams {
				if s.id == slot.id {
					sl.userStreams[userID] = append(streams[:i], streams[i+1:]...)
					break
				}
			}
			if len(sl.userStreams[userID]) == 0 {
				delete(sl.userStreams, userID)
			}
			if sid != "" {
				sl.removeSidSlotLocked(sid, slot.id)
			}
		})
	}

	return streamCtx, release
}

func (sl *StreamLimiter) removeSidSlotLocked(sid string, slotID uint64) {
	slots := sl.sidStreams[sid]
	for i, s := range slots {
		if s.id == slotID {
			sl.sidStreams[sid] = append(slots[:i], slots[i+1:]...)
			break
		}
	}
	if len(sl.sidStreams[sid]) == 0 {
		delete(sl.sidStreams, sid)
	}
}

// CloseUser cancels and closes all active streams for userID.
func (sl *StreamLimiter) CloseUser(userID string) {
	if userID == "" {
		return
	}
	sl.mu.Lock()
	slots := append([]*streamSlot(nil), sl.userStreams[userID]...)
	sl.mu.Unlock()

	for _, s := range slots {
		s.cancel()
	}
}

// CloseSession cancels and closes all active streams for sid.
func (sl *StreamLimiter) CloseSession(sid string) {
	if sid == "" {
		return
	}
	sl.mu.Lock()
	slots := append([]*streamSlot(nil), sl.sidStreams[sid]...)
	sl.mu.Unlock()

	for _, s := range slots {
		s.cancel()
	}
}

// allowStreamOpen checks if a new stream open attempt is permitted under the rate limit.
// Returns true and 0 if allowed; false and retry-after seconds if rate limit exceeded.
func (sl *StreamLimiter) allowStreamOpen(userID string) (bool, int) {
	return sl.allowStreamOpenAt(userID, time.Now())
}

// allowStreamOpenAt allows passing an explicit timestamp for deterministic testing.
func (sl *StreamLimiter) allowStreamOpenAt(userID string, now time.Time) (bool, int) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	cutoff := now.Add(-sl.window)
	attempts := sl.openAttempts[userID]

	valid := make([]time.Time, 0, len(attempts))
	for _, t := range attempts {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= sl.rateLimit {
		oldest := valid[0]
		remaining := oldest.Add(sl.window).Sub(now)
		secs := int(remaining.Seconds())
		if secs < 1 {
			secs = 1
		}
		sl.openAttempts[userID] = valid
		return false, secs
	}

	valid = append(valid, now)
	sl.openAttempts[userID] = valid
	return true, 0
}

// ActiveSlots returns the count of active stream slots for userID.
func (sl *StreamLimiter) ActiveSlots(userID string) int {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return len(sl.userStreams[userID])
}

// TotalActiveSlots returns the total active slots across all accounts.
func (sl *StreamLimiter) TotalActiveSlots() int {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	total := 0
	for _, slots := range sl.userStreams {
		total += len(slots)
	}
	return total
}

// Server wires notification dependencies.
type Server struct {
	Store             store.Store
	Bus               bus.Bus
	GatewaySecret     string
	InternalToken     string
	Limiter           *StreamLimiter
	HeartbeatInterval time.Duration
	CheckEveryNth     int
	accountSubCancel  context.CancelFunc
	accountSubDone    chan struct{}
}

// New creates a Server with safe default stream limits (cap: 3, rate: 10/min).
func New(st store.Store, b bus.Bus, gatewaySecret, internalToken string) *Server {
	return &Server{
		Store:             st,
		Bus:               b,
		GatewaySecret:     gatewaySecret,
		InternalToken:     internalToken,
		Limiter:           NewStreamLimiter(3, 10, time.Minute),
		HeartbeatInterval: 25 * time.Second,
		CheckEveryNth:     1,
	}
}

// StartAccountEventsListener subscribes once to account:events and closes
// matching active streams when revocation events arrive.
func (s *Server) StartAccountEventsListener(ctx context.Context) func() {
	if s.Bus == nil {
		return func() {}
	}
	subCtx, cancel := context.WithCancel(ctx)
	s.accountSubCancel = cancel
	s.accountSubDone = make(chan struct{})

	go func() {
		defer close(s.accountSubDone)
		ch, unsub, err := s.Bus.SubscribeAccountEvents(subCtx)
		if err != nil {
			log.Printf("[NOTIF] failed to subscribe to account:events: %v", err)
			return
		}
		defer unsub()

		for {
			select {
			case <-subCtx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				if evt == nil {
					continue
				}
				if evt.SID != "" && s.Limiter != nil {
					s.Limiter.CloseSession(evt.SID)
				}
				if evt.UserID != "" && s.Limiter != nil {
					s.Limiter.CloseUser(evt.UserID)
				}
			}
		}
	}()

	return func() {
		cancel()
		<-s.accountSubDone
	}
}

// StopAccountEventsListener stops the account events listener if active.
func (s *Server) StopAccountEventsListener() {
	if s.accountSubCancel != nil {
		s.accountSubCancel()
		if s.accountSubDone != nil {
			<-s.accountSubDone
		}
	}
}

var logWriteDeadlineUnsupported sync.Once

func (s *Server) setWriteDeadline(w http.ResponseWriter, deadline time.Time) {
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(deadline); err != nil {
		if errors.Is(err, http.ErrNotSupported) {
			logWriteDeadlineUnsupported.Do(func() {
				log.Printf("[NOTIF] ResponseController.SetWriteDeadline not supported")
			})
		}
	}
}

func (s *Server) clearWriteDeadline(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		if errors.Is(err, http.ErrNotSupported) {
			return
		}
	}
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

	streamCtx := r.Context()
	if s.Limiter != nil {
		if allowed, retryAfter := s.Limiter.allowStreamOpen(claims.UserID); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			handlerutil.WriteSafeError(w, r, http.StatusTooManyRequests, "rate_limited", "stream open rate limit exceeded", nil)
			return
		}

		var release func()
		streamCtx, release = s.Limiter.acquireStreamSlot(r.Context(), claims.UserID, claims.SID)
		defer release()
	}
	ch, unsub, err := s.Bus.Subscribe(streamCtx, claims.UserID)
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

	s.setWriteDeadline(w, time.Now().Add(10*time.Second))
	if _, err := fmt.Fprintf(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()
	s.clearWriteDeadline(w)

	interval := s.HeartbeatInterval
	if interval <= 0 {
		interval = 25 * time.Second
	}
	checkNth := s.CheckEveryNth
	if checkNth <= 0 {
		checkNth = 1
	}

	heartbeat := time.NewTicker(interval)
	defer heartbeat.Stop()
	heartbeatCount := 0
	for {
		select {
		case <-streamCtx.Done():
			return
		case n, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(n)
			if err != nil {
				continue
			}
			s.setWriteDeadline(w, time.Now().Add(10*time.Second))
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
			s.clearWriteDeadline(w)
		case <-heartbeat.C:
			heartbeatCount++
			if heartbeatCount%checkNth == 0 {
				if err := jwtutil.CheckRevocation(claims); err != nil {
					if errors.Is(err, jwtutil.ErrSessionRevoked) || errors.Is(err, jwtutil.ErrTokenRevoked) {
						// Session or user token revoked: close stream cleanly.
						return
					}
					// For any other error (transient Redis outage / lookup failure): log a warning,
					// keep the stream open, and retry on the next heartbeat check to prevent thundering herds.
					// Note: Never log the full token or full sid.
					log.Printf("[NOTIF] warning: Redis error during SSE heartbeat revocation check for user %s: %v", claims.UserID, err)
				}
			}
			s.setWriteDeadline(w, time.Now().Add(10*time.Second))
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
			s.clearWriteDeadline(w)
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
	SubjectID   string `json:"subject_id"`
}

// subjectIDPattern constrains the optional subject id carried on a push: the
// same shape academy-service uses for subject ids (1-100 of letters,
// digits, dash, underscore). Anything else is rejected before any DB call.
var subjectIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// Push serves POST /internal/push for trusted services: persists then fans out.
func (s *Server) Push(w http.ResponseWriter, r *http.Request) {
	var req pushRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.Title) == "" {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid request body", err)
		return
	}
	if req.SubjectID != "" && !subjectIDPattern.MatchString(req.SubjectID) {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_subject_id", "invalid subject id", nil)
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
		SubjectID: req.SubjectID,
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
