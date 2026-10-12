// Admin broadcast API (SPEC Phase 7, ADR-0013).
//
// The admin listener (ADMIN_LISTEN_ADDR, internal network only, mTLS, never
// published) serves ONLY /internal/admin/* routes. Every admin route requires,
// in this order:
//
//  1. X-Internal-Token, constant-time compared against INTERNAL_SERVICE_TOKEN
//     (empty-secret guard, mirroring auth-service and academy-service).
//  2. X-Admin-Token, verified on EVERY request via auth-service
//     POST {AUTH_ADMIN_URL}/internal/admin/verify with a 3 s timeout. No
//     caching, so revocation takes effect immediately (ADR-0008 Section 6).
//     Auth-service down (or any verify failure that is not a clean 401/429)
//     fails closed with 503. Lockout is enforced by auth-service and relayed
//     as 429.
//
// Audience resolution pages through user ids and never loads all users into
// memory: "all" pages auth-service GET /internal/admin/accounts?status=active,
// "subject:<id>" pages academy-service
// GET /internal/admin/subjects/{id}/owner-ids. The calling admin's token is
// forwarded so attribution and lockout stay correct downstream.
//
// Fan-out writes per-user rows into the existing notifications collection
// with deterministic ids per (broadcast, user) and tracks each delivery in
// broadcast_targets (unique per pair), so a retry, a double send or a
// restart never delivers twice. A broadcast left in "sending" resumes on
// startup. One user's failure never stops the broadcast; failed targets are
// retryable. Audit entries carry admin, audience and counts, never message
// text, and no IP address is persisted.
package handlers

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/bus"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/models"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/storesafety"
)

const (
	// broadcastTitleMaxRunes caps each title field (ar and en).
	broadcastTitleMaxRunes = 80
	// broadcastBodyMaxRunes caps each body field (ar and en).
	broadcastBodyMaxRunes = 500
	// broadcastPageSize pages audience resolution and fan-out batches.
	broadcastPageSize = 100
	// BroadcastSendHourlyLimit caps sends per admin per hour.
	BroadcastSendHourlyLimit = 10
	// adminVerifyTimeout bounds every admin token verification call.
	broadcastVerifyTimeout = 3 * time.Second
	// upstreamTimeout bounds audience-resolution calls to auth/academy.
	upstreamTimeout = 10 * time.Second
	// dbTimeout bounds broadcast store calls.
	broadcastDBTimeout = 5 * time.Second
)

// SendLimiter caps broadcast sends per admin. Implementations must answer
// 429-style: Allow reports false with retry-after seconds when over budget.
type SendLimiter interface {
	Allow(adminID string) (bool, int64)
}

// MemorySendLimiter is a fixed-window in-process SendLimiter (dev/test).
// Redis deployments use ratelimit.NewRateLimiter with the same budget.
type MemorySendLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	attempts map[string][]time.Time
	now      func() time.Time
}

// NewMemorySendLimiter creates a fixed-window limiter (limit per window).
func NewMemorySendLimiter(limit int, window time.Duration) *MemorySendLimiter {
	if limit <= 0 {
		limit = BroadcastSendHourlyLimit
	}
	if window <= 0 {
		window = time.Hour
	}
	return &MemorySendLimiter{
		limit:    limit,
		window:   window,
		attempts: map[string][]time.Time{},
		now:      time.Now,
	}
}

// Allow records an attempt and reports whether it fits the budget.
func (l *MemorySendLimiter) Allow(adminID string) (bool, int64) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	valid := make([]time.Time, 0, len(l.attempts[adminID]))
	for _, t := range l.attempts[adminID] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	if len(valid) >= l.limit {
		secs := int64(valid[0].Add(l.window).Sub(now).Seconds()) + 1
		if secs < 1 {
			secs = 1
		}
		l.attempts[adminID] = valid
		return false, secs
	}
	l.attempts[adminID] = append(valid, now)
	return true, 0
}

// RedisSendLimiter adapts shared/infra/ratelimit.RateLimiter (fixed window
// with a short lockout on breach, fail closed) to SendLimiter.
type RedisSendLimiter struct {
	check func(key string) (bool, time.Duration)
}

// NewRedisSendLimiter wraps a check function (normally RateLimiter.CheckAndRecord).
func NewRedisSendLimiter(check func(key string) (bool, time.Duration)) *RedisSendLimiter {
	return &RedisSendLimiter{check: check}
}

// Allow records an attempt; over budget answers false with retry seconds.
func (l *RedisSendLimiter) Allow(adminID string) (bool, int64) {
	limited, ttl := l.check("broadcast-send:" + adminID)
	if limited {
		secs := int64(ttl.Seconds()) + 1
		if secs < 1 {
			secs = 1
		}
		return false, secs
	}
	return true, 0
}

// AdminIdentity is the verified admin operator attached to admin requests.
type AdminIdentity struct {
	ID   string
	Name string
}

type adminContextKey string

const broadcastAdminContextKey adminContextKey = "admin"

// AdminServer serves the broadcast admin API on the internal admin listener.
type AdminServer struct {
	Store           store.Store
	Bus             bus.Bus
	InternalToken   string
	AuthAdminURL    string
	AcademyAdminURL string
	// VerifyClient verifies admin tokens (3 s timeout, mTLS in prod).
	VerifyClient *http.Client
	// UpstreamClient pages auth/academy for audience resolution.
	UpstreamClient *http.Client
	// Limiter caps sends per admin per hour.
	Limiter SendLimiter

	mu      sync.Mutex
	running map[string]bool
}

// AdminFromBroadcastRequest returns the verified admin identity from context.
func AdminFromBroadcastRequest(r *http.Request) (*AdminIdentity, bool) {
	if adm, ok := r.Context().Value(broadcastAdminContextKey).(*AdminIdentity); ok && adm != nil {
		return adm, true
	}
	return nil, false
}

func (a *AdminServer) verifyHTTPClient() *http.Client {
	if a.VerifyClient != nil {
		return a.VerifyClient
	}
	return &http.Client{Timeout: broadcastVerifyTimeout}
}

func (a *AdminServer) upstreamHTTPClient() *http.Client {
	if a.UpstreamClient != nil {
		return a.UpstreamClient
	}
	return &http.Client{Timeout: upstreamTimeout}
}

// verifyAdminToken validates an admin token against auth-service
// POST {AUTH_ADMIN_URL}/internal/admin/verify. No caching. It returns the
// identity on success, otherwise an HTTP status and safe code.
func (a *AdminServer) verifyAdminToken(r *http.Request, adminToken string) (*AdminIdentity, int, string) {
	base := strings.TrimRight(strings.TrimSpace(a.AuthAdminURL), "/")
	if base == "" || a.InternalToken == "" {
		return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
	}

	ctx, cancel := context.WithTimeout(r.Context(), broadcastVerifyTimeout)
	defer cancel()

	// #nosec G704 //nolint:gosec -- base URL comes from validated service config, path is fixed
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/internal/admin/verify", nil)
	if err != nil {
		return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
	}
	req.Header.Set("X-Internal-Token", a.InternalToken)
	req.Header.Set("X-Admin-Token", adminToken)
	// Forwarded so auth-service lockout keys on the real client address as set
	// by admin-console. Never trusted locally for auth decisions.
	if clientIP := strings.TrimSpace(r.Header.Get("X-Admin-Client-IP")); clientIP != "" {
		req.Header.Set("X-Admin-Client-IP", clientIP)
	}

	resp, err := a.verifyHTTPClient().Do(req)
	if err != nil {
		return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		var v struct {
			AdminID string `json:"admin_id"`
			Name    string `json:"name"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v); err != nil {
			return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
		}
		if strings.TrimSpace(v.AdminID) == "" || strings.TrimSpace(v.Name) == "" {
			return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
		}
		return &AdminIdentity{ID: v.AdminID, Name: v.Name}, http.StatusOK, ""
	case http.StatusUnauthorized:
		return nil, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized
	case http.StatusTooManyRequests:
		return nil, http.StatusTooManyRequests, "locked_out"
	default:
		return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
	}
}

// requireAdmin enforces the two-token admin auth chain: X-Internal-Token
// (constant-time, empty-secret guard), then X-Admin-Token verified via
// auth-service. Auth-service lockout relays as 429; auth down fails closed
// as 503.
func (a *AdminServer) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Internal-Token")
		if a.InternalToken == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(a.InternalToken)) != 1 {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		}

		adminToken := strings.TrimSpace(r.Header.Get("X-Admin-Token"))
		if adminToken == "" {
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		}

		adm, status, code := a.verifyAdminToken(r, adminToken)
		switch status {
		case http.StatusOK:
			ctx := context.WithValue(r.Context(), broadcastAdminContextKey, adm)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		case http.StatusTooManyRequests:
			handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "too many attempts, retry later",
				"code":  code,
			})
			return
		case http.StatusUnauthorized:
			handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
			return
		default:
			handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", nil)
			return
		}
	})
}

// sanitizeBroadcastDetail strips CR/LF from audit detail text (ADR-0008).
func sanitizeBroadcastDetail(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
}

// writeBroadcastAudit writes one admin_audit_log entry. The detail carries
// admin, audience and counts only — never message text, no IP. An audit
// failure is logged and never fails the admin call.
func (a *AdminServer) writeBroadcastAudit(ctx context.Context, adm *AdminIdentity, action string, b *models.Broadcast) {
	detail := sanitizeBroadcastDetail(fmt.Sprintf("audience=%s total=%d delivered=%d failed=%d", b.Audience, b.Total, b.Delivered, b.Failed))
	id, _ := jwtutil.GenerateUUID()
	entry := &models.AuditLog{
		ID:         id,
		ActorID:    adm.ID,
		ActorName:  adm.Name,
		Action:     action,
		TargetType: "broadcast",
		TargetID:   b.ID,
		Detail:     detail,
		CreatedAt:  time.Now().UTC(),
	}
	dbCtx, cancel := context.WithTimeout(ctx, broadcastDBTimeout)
	defer cancel()
	if err := a.Store.CreateAuditLog(dbCtx, entry); err != nil {
		cleanTarget := strings.ReplaceAll(strings.ReplaceAll(b.ID, "\r", ""), "\n", "")
		// #nosec G706 -- cleanTarget sanitized of CR/LF
		log.Printf("[ERROR] [NOTIF] broadcast audit log write failed for action %s actor_id=%s target_id=%s: %v", action, adm.ID, cleanTarget, err)
	}
}

// decodeBroadcastJSON decodes a strict JSON body (unknown fields refused).
func decodeBroadcastJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, handlerutil.ErrCodeInvalidJSON, "invalid request body", err)
		return false
	}
	return true
}

// parseAudience splits "all" or "subject:<id>". The id must be well-formed;
// existence is checked against academy-service during resolution.
func parseAudience(raw string) (subjectID string, isAll bool, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "all" {
		return "", true, true
	}
	id, found := strings.CutPrefix(raw, "subject:")
	if !found || !subjectIDPattern.MatchString(id) {
		return "", false, false
	}
	return id, false, true
}

// hasControlChars reports control characters other than \n and \t.
func hasControlChars(s string) bool {
	for _, r := range s {
		if r == '\n' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// cleanBroadcastField trims and enforces the rune cap; empty is reported so
// callers can require fields. Control characters are rejected, not stripped.
func cleanBroadcastField(raw string, maxRunes int) (string, bool) {
	s := strings.TrimSpace(raw)
	if hasControlChars(s) {
		return "", false
	}
	if utf8.RuneCountInString(s) > maxRunes {
		return "", false
	}
	return s, true
}

type broadcastCreateRequest struct {
	Audience string `json:"audience"`
	TitleAr  string `json:"title_ar"`
	TitleEn  string `json:"title_en"`
	BodyAr   string `json:"body_ar"`
	BodyEn   string `json:"body_en"`
}

// validatedBroadcast is a create request after server-side validation.
type validatedBroadcast struct {
	audience  string
	subjectID string
	titleAr   string
	titleEn   string
	bodyAr    string
	bodyEn    string
}

// validateBroadcastInput enforces lengths, required Arabic fields, control
// characters and the store-safety word rule on every text field.
func validateBroadcastInput(in *broadcastCreateRequest) (*validatedBroadcast, int, string) {
	subjectID, _, ok := parseAudience(in.Audience)
	if !ok {
		return nil, http.StatusBadRequest, "invalid_audience"
	}
	titleAr, ok := cleanBroadcastField(in.TitleAr, broadcastTitleMaxRunes)
	if !ok || titleAr == "" {
		return nil, http.StatusBadRequest, "invalid_title"
	}
	titleEn, ok := cleanBroadcastField(in.TitleEn, broadcastTitleMaxRunes)
	if !ok {
		return nil, http.StatusBadRequest, "invalid_title"
	}
	bodyAr, ok := cleanBroadcastField(in.BodyAr, broadcastBodyMaxRunes)
	if !ok || bodyAr == "" {
		return nil, http.StatusBadRequest, "invalid_body"
	}
	bodyEn, ok := cleanBroadcastField(in.BodyEn, broadcastBodyMaxRunes)
	if !ok {
		return nil, http.StatusBadRequest, "invalid_body"
	}
	for _, txt := range []string{titleAr, titleEn, bodyAr, bodyEn} {
		if txt != "" && storesafety.HasForbiddenWord(txt) {
			return nil, http.StatusBadRequest, "broadcast_forbidden_word"
		}
	}
	audience := "all"
	if subjectID != "" {
		audience = "subject:" + subjectID
	}
	return &validatedBroadcast{
		audience:  audience,
		subjectID: subjectID,
		titleAr:   titleAr,
		titleEn:   titleEn,
		bodyAr:    bodyAr,
		bodyEn:    bodyEn,
	}, http.StatusOK, ""
}

// audiencePage is one resolved page of recipient user ids.
type audiencePage struct {
	ids        []string
	total      int
	nextCursor string
	done       bool
}

// upstreamGET performs an authenticated GET against auth-service or
// academy-service, forwarding the calling admin's token.
func (a *AdminServer) upstreamGET(ctx context.Context, adminToken, url string) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	// #nosec G704 //nolint:gosec -- base URL comes from validated service config; path segments are validated ids
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Internal-Token", a.InternalToken)
	req.Header.Set("X-Admin-Token", adminToken)
	// #nosec G704 //nolint:gosec -- base URL is validated service config (scheme://host only); the subject id and cursor match the id pattern, auth pages are formatted ints
	resp, err := a.upstreamHTTPClient().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

// resolveAudiencePage returns one page of recipient ids for an audience.
// cursor is "" on the first page: auth pages by number ("1", "2", ...),
// academy resumes from its opaque next_cursor.
func (a *AdminServer) resolveAudiencePage(ctx context.Context, adminToken, audience, cursor string) (*audiencePage, int, string) {
	if audience == "all" {
		base := strings.TrimRight(strings.TrimSpace(a.AuthAdminURL), "/")
		if base == "" {
			return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
		}
		page := 1
		if cursor != "" {
			p, err := strconv.Atoi(cursor)
			if err != nil || p < 1 {
				return nil, http.StatusBadRequest, "invalid_audience"
			}
			page = p
		}
		url := fmt.Sprintf("%s/internal/admin/accounts?status=active&page=%d&limit=%d", base, page, broadcastPageSize)
		status, body, err := a.upstreamGET(ctx, adminToken, url)
		if err != nil || status != http.StatusOK {
			return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
		}
		var v struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			Total int `json:"total"`
			Page  int `json:"page"`
			Limit int `json:"limit"`
		}
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&v); err != nil {
			return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
		}
		ids := make([]string, 0, len(v.Items))
		for _, it := range v.Items {
			if strings.TrimSpace(it.ID) != "" {
				ids = append(ids, it.ID)
			}
		}
		perPage := v.Limit
		if perPage <= 0 {
			perPage = broadcastPageSize
		}
		done := page*perPage >= v.Total || len(v.Items) == 0
		next := ""
		if !done {
			next = strconv.Itoa(page + 1)
		}
		return &audiencePage{ids: ids, total: v.Total, nextCursor: next, done: done}, http.StatusOK, ""
	}

	subjectID, _, ok := parseAudience(audience)
	if !ok {
		return nil, http.StatusBadRequest, "invalid_audience"
	}
	base := strings.TrimRight(strings.TrimSpace(a.AcademyAdminURL), "/")
	if base == "" {
		return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
	}
	// The cursor is an opaque academy-issued user id; refuse anything else
	// so only validated parts reach the upstream URL.
	if cursor != "" && !subjectIDPattern.MatchString(cursor) {
		return nil, http.StatusBadRequest, "invalid_audience"
	}
	url := fmt.Sprintf("%s/internal/admin/subjects/%s/owner-ids?limit=%d", base, subjectID, broadcastPageSize)
	if cursor != "" {
		url += "&cursor=" + cursor
	}
	status, body, err := a.upstreamGET(ctx, adminToken, url)
	if err != nil {
		return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
	}
	if status == http.StatusNotFound {
		return nil, http.StatusNotFound, "subject_not_found"
	}
	if status != http.StatusOK {
		return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
	}
	var v struct {
		UserIDs    []string `json:"user_ids"`
		NextCursor string   `json:"next_cursor"`
		Total      int      `json:"total"`
	}
	// Non-strict decode: upstream responses may carry extra fields.
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&v); err != nil {
		return nil, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable
	}
	ids := make([]string, 0, len(v.UserIDs))
	for _, id := range v.UserIDs {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	return &audiencePage{ids: ids, total: v.Total, nextCursor: v.NextCursor, done: v.NextCursor == ""}, http.StatusOK, ""
}

// storeTargets upserts one resolved page as pending targets.
func (a *AdminServer) storeTargets(ctx context.Context, broadcastID string, ids []string) error {
	for _, uid := range ids {
		dbCtx, cancel := context.WithTimeout(ctx, broadcastDBTimeout)
		_, err := a.Store.UpsertBroadcastTarget(dbCtx, &models.BroadcastTarget{
			ID:          store.TargetID(broadcastID, uid),
			BroadcastID: broadcastID,
			UserID:      uid,
			Status:      models.TargetPending,
			UpdatedAt:   time.Now(),
		})
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

// broadcastRowID is the deterministic notification id per (broadcast, user).
func broadcastRowID(broadcastID, userID string) string {
	return "broadcast:" + broadcastID + ":" + userID
}

// deliverTarget writes one per-user row and publishes it live. A duplicate
// row id means a previous attempt already stored it: publishing is retried
// and delivery counts as sent.
func (a *AdminServer) deliverTarget(ctx context.Context, b *models.Broadcast, userID string) error {
	n := &models.Notification{
		ID:        broadcastRowID(b.ID, userID),
		UserID:    userID,
		Title:     b.Title,
		TitleAr:   b.TitleAr,
		Body:      b.Body,
		BodyAr:    b.BodyAr,
		Type:      "event",
		SubjectID: b.SubjectID,
	}
	dbCtx, cancel := context.WithTimeout(ctx, broadcastDBTimeout)
	err := a.Store.Create(dbCtx, n)
	cancel()
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	if err := a.Bus.Publish(ctx, n); err != nil {
		return err
	}
	return nil
}

// runActive reports whether a fan-out run holds the in-process lock now.
func (a *AdminServer) runActive(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running != nil && a.running[id]
}

// tryStartRun claims the in-process run lock for a broadcast.
func (a *AdminServer) tryStartRun(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running == nil {
		a.running = map[string]bool{}
	}
	if a.running[id] {
		return false
	}
	a.running[id] = true
	return true
}

func (a *AdminServer) endRun(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.running, id)
}

// reconcileCounts re-derives delivered/failed from target rows so a restart
// between marking and persisting never drifts the counts.
func (a *AdminServer) reconcileCounts(ctx context.Context, b *models.Broadcast) {
	dbCtx, cancel := context.WithTimeout(ctx, broadcastDBTimeout)
	defer cancel()
	if n, err := a.Store.CountTargets(dbCtx, b.ID, models.TargetSent); err == nil {
		b.Delivered = n
	}
	if n, err := a.Store.CountTargets(dbCtx, b.ID, models.TargetFailed); err == nil {
		b.Failed = n
	}
}

func (a *AdminServer) persistBroadcast(ctx context.Context, b *models.Broadcast) error {
	dbCtx, cancel := context.WithTimeout(ctx, broadcastDBTimeout)
	defer cancel()
	return a.Store.UpdateBroadcast(dbCtx, b)
}

// runBroadcast resolves the audience and fans out in bounded batches. It is
// idempotent: resolution upserts targets, delivery tolerates duplicate rows,
// and a broadcast left in sending resumes after a restart. An empty admin
// token (startup resume) skips resolution and finalization: already-resolved
// pending targets are still delivered, and resolution completes on the next
// admin retry, which always carries a token.
func (a *AdminServer) runBroadcast(ctx context.Context, adminToken string, id string) {
	if !a.tryStartRun(id) {
		return
	}
	defer a.endRun(id)

	// Strip CR/LF before the id reaches any log line (G706).
	cleanID := strings.ReplaceAll(strings.ReplaceAll(id, "\r", ""), "\n", "")

	dbCtx, cancel := context.WithTimeout(ctx, broadcastDBTimeout)
	b, err := a.Store.GetBroadcast(dbCtx, id)
	cancel()
	if err != nil {
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[ERROR] [NOTIF] broadcast run %s: load: %v", cleanID, err)
		return
	}
	if b.Status == models.BroadcastDone || b.Status == models.BroadcastPartial || b.Status == models.BroadcastFailed {
		return
	}
	b.Status = models.BroadcastSending
	a.reconcileCounts(ctx, b)
	if err := a.persistBroadcast(ctx, b); err != nil {
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[ERROR] [NOTIF] broadcast run %s: persist: %v", cleanID, err)
		return
	}

	// Resolve phase: page the audience into pending targets.
	if adminToken != "" {
		for !b.ResolveDone {
			page, status, _ := a.resolveAudiencePage(ctx, adminToken, b.Audience, b.ResolveCursor)
			if status != http.StatusOK || page == nil {
				// #nosec G706 -- cleanID sanitized of CR/LF
				log.Printf("[ERROR] [NOTIF] broadcast run %s: resolve audience: status %d", cleanID, status)
				return
			}
			if err := a.storeTargets(ctx, b.ID, page.ids); err != nil {
				// #nosec G706 -- cleanID sanitized of CR/LF
				log.Printf("[ERROR] [NOTIF] broadcast run %s: store targets: %v", cleanID, err)
				return
			}
			b.Total = page.total
			b.ResolveCursor = page.nextCursor
			b.ResolveDone = page.done
			if err := a.persistBroadcast(ctx, b); err != nil {
				// #nosec G706 -- cleanID sanitized of CR/LF
				log.Printf("[ERROR] [NOTIF] broadcast run %s: persist resolve: %v", cleanID, err)
				return
			}
		}
	} else if !b.ResolveDone {
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[ERROR] [NOTIF] broadcast run %s: resolution deferred until an admin retry (no token at resume)", cleanID)
	}

	// Send phase: bounded batches of pending targets.
	for {
		dbCtx, cancel := context.WithTimeout(ctx, broadcastDBTimeout)
		batch, err := a.Store.ListPendingTargets(dbCtx, b.ID, broadcastPageSize)
		cancel()
		if err != nil {
			// #nosec G706 -- cleanID sanitized of CR/LF
			log.Printf("[ERROR] [NOTIF] broadcast run %s: list pending: %v", cleanID, err)
			return
		}
		if len(batch) == 0 {
			break
		}
		for _, t := range batch {
			if err := a.deliverTarget(ctx, b, t.UserID); err != nil {
				cleanErr := sanitizeBroadcastDetail(err.Error())
				if len(cleanErr) > 200 {
					cleanErr = cleanErr[:200]
				}
				dbCtx, cancel := context.WithTimeout(ctx, broadcastDBTimeout)
				merr := a.Store.MarkBroadcastTarget(dbCtx, b.ID, t.UserID, models.TargetFailed, cleanErr)
				cancel()
				if merr != nil {
					// #nosec G706 -- cleanID sanitized of CR/LF
					log.Printf("[ERROR] [NOTIF] broadcast run %s: mark failed: %v", cleanID, merr)
					return
				}
				b.Failed++
			} else {
				dbCtx, cancel := context.WithTimeout(ctx, broadcastDBTimeout)
				merr := a.Store.MarkBroadcastTarget(dbCtx, b.ID, t.UserID, models.TargetSent, "")
				cancel()
				if merr != nil {
					// #nosec G706 -- cleanID sanitized of CR/LF
					log.Printf("[ERROR] [NOTIF] broadcast run %s: mark sent: %v", cleanID, merr)
					return
				}
				b.Delivered++
			}
		}
		if err := a.persistBroadcast(ctx, b); err != nil {
			// #nosec G706 -- cleanID sanitized of CR/LF
			log.Printf("[ERROR] [NOTIF] broadcast run %s: persist counts: %v", cleanID, err)
			return
		}
	}

	switch {
	case !b.ResolveDone:
		// Tokenless resume: delivery of resolved targets is done; the rest
		// completes on the next admin retry.
		b.Status = models.BroadcastSending
	case b.Failed == 0:
		b.Status = models.BroadcastDone
	case b.Delivered > 0:
		b.Status = models.BroadcastPartial
	default:
		b.Status = models.BroadcastFailed
	}
	if err := a.persistBroadcast(ctx, b); err != nil {
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[ERROR] [NOTIF] broadcast run %s: persist final: %v", cleanID, err)
	}
}

// ResumeBroadcasts restarts delivery for broadcasts left queued or sending
// (e.g. after a process restart). It carries no admin token, so only
// already-resolved pending targets are delivered; resolution of the rest
// completes on the next admin retry.
func (a *AdminServer) ResumeBroadcasts(ctx context.Context) {
	dbCtx, cancel := context.WithTimeout(ctx, broadcastDBTimeout)
	items, _, err := a.Store.ListBroadcasts(dbCtx, 1, 100)
	cancel()
	if err != nil {
		log.Printf("[ERROR] [NOTIF] broadcast resume: list: %v", err)
		return
	}
	for _, b := range items {
		if b.Status == models.BroadcastSending || b.Status == models.BroadcastQueued {
			go a.runBroadcast(context.WithoutCancel(ctx), "", b.ID)
		}
	}
}

// PreviewBroadcast handles POST /internal/admin/broadcasts/preview: a dry
// run returning the recipient count. Nothing is stored or sent.
func (a *AdminServer) PreviewBroadcast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if _, ok := AdminFromBroadcastRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	var in struct {
		Audience string `json:"audience"`
	}
	if !decodeBroadcastJSON(w, r, &in) {
		return
	}
	if _, _, ok := parseAudience(in.Audience); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "invalid_audience", "invalid audience", nil)
		return
	}
	page, status, code := a.resolveAudiencePage(r.Context(), r.Header.Get("X-Admin-Token"), strings.TrimSpace(in.Audience), "")
	if status != http.StatusOK || page == nil {
		if status == http.StatusNotFound {
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, code, "subject not found", nil)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", nil)
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"audience":        strings.TrimSpace(in.Audience),
		"recipient_count": page.total,
	})
}

// CreateBroadcast handles POST /internal/admin/broadcasts: validates,
// rate limits (10/hour/admin, 429 + Retry-After), resolves the first
// audience page synchronously (unknown subject -> 404, empty audience ->
// 400, nothing sent), stores the broadcast, audits the send, and fans out
// asynchronously.
func (a *AdminServer) CreateBroadcast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	adm, ok := AdminFromBroadcastRequest(r)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	var in broadcastCreateRequest
	if !decodeBroadcastJSON(w, r, &in) {
		return
	}
	v, status, code := validateBroadcastInput(&in)
	if status != http.StatusOK {
		handlerutil.WriteSafeError(w, r, status, code, "invalid broadcast", nil)
		return
	}
	if a.Limiter != nil {
		if ok, retry := a.Limiter.Allow(adm.ID); !ok {
			w.Header().Set("Retry-After", strconv.FormatInt(retry, 10))
			handlerutil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "too many broadcast sends, retry later",
				"code":  "rate_limited",
			})
			return
		}
	}

	adminToken := r.Header.Get("X-Admin-Token")
	page, status, code := a.resolveAudiencePage(r.Context(), adminToken, v.audience, "")
	if status != http.StatusOK || page == nil {
		if status == http.StatusNotFound {
			handlerutil.WriteSafeError(w, r, http.StatusNotFound, code, "subject not found", nil)
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", nil)
		return
	}
	if page.total == 0 {
		handlerutil.WriteSafeError(w, r, http.StatusBadRequest, "empty_audience", "audience has no recipients", nil)
		return
	}

	id, _ := jwtutil.GenerateUUID()
	now := time.Now().UTC()
	b := &models.Broadcast{
		ID:            id,
		Audience:      v.audience,
		Title:         v.titleEn,
		TitleAr:       v.titleAr,
		Body:          v.bodyEn,
		BodyAr:        v.bodyAr,
		SubjectID:     v.subjectID,
		CreatedBy:     adm.ID,
		CreatedAt:     now,
		Status:        models.BroadcastQueued,
		Total:         page.total,
		ResolveCursor: page.nextCursor,
		ResolveDone:   page.done,
	}
	// English UI must never show an empty row: fall back to Arabic.
	if b.Title == "" {
		b.Title = b.TitleAr
	}
	if b.Body == "" {
		b.Body = b.BodyAr
	}
	dbCtx, cancel := context.WithTimeout(r.Context(), broadcastDBTimeout)
	err := a.Store.CreateBroadcast(dbCtx, b)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if err := a.storeTargets(r.Context(), b.ID, page.ids); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	a.writeBroadcastAudit(r.Context(), adm, "broadcast_send", b)
	go a.runBroadcast(context.WithoutCancel(r.Context()), adminToken, b.ID)
	handlerutil.WriteJSON(w, http.StatusCreated, map[string]any{
		"id":        b.ID,
		"status":    b.Status,
		"total":     b.Total,
		"delivered": b.Delivered,
		"failed":    b.Failed,
	})
}

// ListBroadcasts handles GET /internal/admin/broadcasts?page=&limit=.
func (a *AdminServer) ListBroadcasts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if _, ok := AdminFromBroadcastRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	page, limit := broadcastPageLimit(r)
	dbCtx, cancel := context.WithTimeout(r.Context(), broadcastDBTimeout)
	items, total, err := a.Store.ListBroadcasts(dbCtx, page, limit)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if items == nil {
		items = []*models.Broadcast{}
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// broadcastPageLimit parses page/limit like the other admin list routes.
func broadcastPageLimit(r *http.Request) (int, int) {
	const maxPage = 10000
	q := r.URL.Query()
	page := 1
	if pStr := strings.TrimSpace(q.Get("page")); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil {
			switch {
			case p < 1:
				page = 1
			case p > maxPage:
				page = maxPage
			default:
				page = p
			}
		}
	}
	limit := 20
	if lStr := strings.TrimSpace(q.Get("limit")); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil {
			switch {
			case l <= 0:
				limit = 20
			case l > 100:
				limit = 100
			default:
				limit = l
			}
		}
	}
	return page, limit
}

// broadcastIDFromPath extracts the id from /internal/admin/broadcasts/{id}[/retry].
func broadcastIDFromPath(path string) (id, rest string, ok bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/internal/admin/broadcasts/"), "/")
	if trimmed == "" {
		return "", "", false
	}
	if id, found := strings.CutSuffix(trimmed, "/retry"); found {
		id = strings.Trim(id, "/")
		if id == "" || strings.Contains(id, "/") {
			return "", "", false
		}
		return id, "retry", true
	}
	if strings.Contains(trimmed, "/") {
		return "", "", false
	}
	return trimmed, "", true
}

// GetBroadcast handles GET /internal/admin/broadcasts/{id}: one broadcast
// with its counts.
func (a *AdminServer) GetBroadcast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if _, ok := AdminFromBroadcastRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	id, rest, ok := broadcastIDFromPath(r.URL.Path)
	if !ok || rest != "" {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
		return
	}
	dbCtx, cancel := context.WithTimeout(r.Context(), broadcastDBTimeout)
	b, err := a.Store.GetBroadcast(dbCtx, id)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "broadcast_not_found", "broadcast not found", nil)
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, b)
}

// RetryBroadcast handles POST /internal/admin/broadcasts/{id}/retry: failed
// targets go back to pending and fan-out resumes. A broadcast still sending
// answers 409; one with nothing failed answers 200 with retried 0.
func (a *AdminServer) RetryBroadcast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	adm, ok := AdminFromBroadcastRequest(r)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	id, rest, ok := broadcastIDFromPath(r.URL.Path)
	if !ok || rest != "retry" {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
		return
	}
	dbCtx, cancel := context.WithTimeout(r.Context(), broadcastDBTimeout)
	b, err := a.Store.GetBroadcast(dbCtx, id)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "broadcast_not_found", "broadcast not found", nil)
		return
	}
	// A live run answers 409; anything else may retry (failed targets or an
	// unfinished resolution).
	if a.runActive(id) {
		handlerutil.WriteSafeError(w, r, http.StatusConflict, "broadcast_in_progress", "broadcast still sending", nil)
		return
	}
	dbCtx, cancel = context.WithTimeout(r.Context(), broadcastDBTimeout)
	n, err := a.Store.ResetFailedTargets(dbCtx, b.ID)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if n == 0 && b.ResolveDone {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"id":        b.ID,
			"status":    b.Status,
			"retried":   0,
			"total":     b.Total,
			"delivered": b.Delivered,
			"failed":    b.Failed,
		})
		return
	}
	b.Status = models.BroadcastSending
	if err := a.persistBroadcast(r.Context(), b); err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	a.writeBroadcastAudit(r.Context(), adm, "broadcast_retry", b)
	go a.runBroadcast(context.WithoutCancel(r.Context()), r.Header.Get("X-Admin-Token"), b.ID)
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"id":        b.ID,
		"status":    b.Status,
		"retried":   n,
		"total":     b.Total,
		"delivered": b.Delivered,
		"failed":    b.Failed,
	})
}

// BroadcastAuditLogs handles GET /internal/admin/audit-log?page=&limit=,
// the notification-service audit log (broadcast sends and retries).
func (a *AdminServer) BroadcastAuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if _, ok := AdminFromBroadcastRequest(r); !ok {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	page, limit := broadcastPageLimit(r)
	dbCtx, cancel := context.WithTimeout(r.Context(), broadcastDBTimeout)
	logs, total, err := a.Store.ListAuditLogs(dbCtx, page, limit)
	cancel()
	if err != nil {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
		return
	}
	if logs == nil {
		logs = []*models.AuditLog{}
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"items": logs,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// AdminHandler constructs the HTTP handler for the internal admin listener.
// It serves ONLY /internal/admin/* routes behind X-Internal-Token +
// X-Admin-Token (verified via auth-service, fail closed) with a 1 MiB body
// cap, and 404s on everything else.
func (a *AdminServer) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/admin/audit-log", a.BroadcastAuditLogs)
	mux.HandleFunc("/internal/admin/broadcasts", a.broadcastCollection)
	// Exact pattern: wins over the broadcasts/ subtree so "preview" is
	// never read as a broadcast id.
	mux.HandleFunc("/internal/admin/broadcasts/preview", a.PreviewBroadcast)
	mux.HandleFunc("/internal/admin/broadcasts/", a.broadcastSubroute)

	var h http.Handler = mux
	h = a.requireAdmin(h)
	h = handlerutil.MaxBytesMiddleware(1 << 20)(h)
	return h
}

// broadcastCollection dispatches the exact /internal/admin/broadcasts
// collection path by method. (Preview has its own exact pattern above.)
func (a *AdminServer) broadcastCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.ListBroadcasts(w, r)
		return
	case http.MethodPost:
		a.CreateBroadcast(w, r)
		return
	}
	handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
}

// broadcastSubroute dispatches /internal/admin/broadcasts/{id} and
// /internal/admin/broadcasts/{id}/retry.
func (a *AdminServer) broadcastSubroute(w http.ResponseWriter, r *http.Request) {
	id, rest, ok := broadcastIDFromPath(r.URL.Path)
	if !ok {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
		return
	}
	_ = id
	switch rest {
	case "":
		a.GetBroadcast(w, r)
	case "retry":
		a.RetryBroadcast(w, r)
	default:
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
	}
}
