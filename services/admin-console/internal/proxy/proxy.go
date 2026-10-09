// Package proxy forwards a fixed set of admin requests to the
// /internal/admin/* endpoints of auth-service and academy-service (ADR-0008).
//
// The console makes no authorization decision: it requires an X-Admin-Token,
// validates the shape of the input, adds the internal token and the real
// client address, and lets the upstream decide. Nothing is passed through
// generically: every handler maps to exactly one upstream endpoint, and the
// upstream request is built from scratch, so no client header (including a
// forged X-Internal-Token or X-Admin-Client-IP) can reach it.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Header names exchanged with auth-service (ADR-0008 sections 4 and 8).
const (
	AdminTokenHeader    = "X-Admin-Token"
	InternalTokenHeader = "X-Internal-Token"
	ClientIPHeader      = "X-Admin-Client-IP"
)

const (
	// UpstreamTimeout bounds every upstream call.
	UpstreamTimeout = 10 * time.Second

	maxRequestBody  = 1 << 20 // 1 MiB
	maxResponseBody = 2 << 20 // 2 MiB
	maxTokenLen     = 512
	maxReasonRunes  = 1000
	maxSearchRunes  = 100
	maxPage         = 1_000_000
	maxLimit        = 100
)

var (
	// User IDs are lower-case UUIDv4 strings (jwtutil.GenerateUUID).
	idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	statusFilters = map[string]bool{"active": true, "suspended": true, "deleted": true, "pending_deletion": true}
)

// Options configures a Proxy.
type Options struct {
	// InternalToken is sent as X-Internal-Token. Required.
	InternalToken string
	// AuthURL is the auth-service admin listener, scheme://host[:port]. Required.
	AuthURL string
	// AcademyURL is the academy-service admin listener, scheme://host[:port].
	// Required: the catalog routes forward to it.
	AcademyURL string
	// TrustedProxies are the peers whose X-Forwarded-For is believed.
	TrustedProxies []netip.Prefix
	// Client performs upstream calls (mTLS in production). Nil uses a default
	// client. Redirects are never followed and the timeout below is enforced.
	Client *http.Client
	// Timeout overrides UpstreamTimeout (tests only).
	Timeout time.Duration
	// MaxPDFBytes caps one uploaded PDF (MAX_PDF_BYTES). Only the upload
	// route accepts a body above 1 MiB: MaxPDFBytes plus the form overhead.
	MaxPDFBytes int64
	// UploadTimeout overrides UploadTimeout (tests only).
	UploadTimeout time.Duration
}

// Proxy holds the immutable state shared by all handlers.
type Proxy struct {
	internalToken string
	authURL       string
	academyURL    string
	trusted       []netip.Prefix
	client        *http.Client
	timeout       time.Duration
	// uploadClient is client with the longer upload timeout.
	uploadClient  *http.Client
	uploadTimeout time.Duration
	maxPDFBytes   int64
}

// New validates the options and returns a Proxy. It fails fast on an empty
// secret or upstream so a misconfigured console cannot start.
func New(o Options) (*Proxy, error) {
	if strings.TrimSpace(o.InternalToken) == "" {
		return nil, errors.New("proxy: internal token is empty")
	}
	base := strings.TrimRight(strings.TrimSpace(o.AuthURL), "/")
	if base == "" {
		return nil, errors.New("proxy: auth admin URL is empty")
	}
	academy := strings.TrimRight(strings.TrimSpace(o.AcademyURL), "/")
	if academy == "" {
		return nil, errors.New("proxy: academy admin URL is empty")
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = UpstreamTimeout
	}
	var c http.Client
	if o.Client != nil {
		c = *o.Client
	}
	c.Timeout = timeout
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	uploadTimeout := o.UploadTimeout
	if uploadTimeout <= 0 {
		uploadTimeout = UploadTimeout
	}
	uc := c
	uc.Timeout = uploadTimeout
	maxPDF := o.MaxPDFBytes
	if maxPDF <= 0 {
		maxPDF = DefaultMaxPDFBytes
	}
	return &Proxy{
		internalToken: o.InternalToken,
		authURL:       base,
		academyURL:    academy,
		trusted:       o.TrustedProxies,
		client:        &c,
		timeout:       timeout,
		uploadClient:  &uc,
		uploadTimeout: uploadTimeout,
		maxPDFBytes:   maxPDF,
	}, nil
}

// ---------------------------------------------------------------------------
// Handlers (one per allowlisted route)
// ---------------------------------------------------------------------------

// Whoami handles GET /api/whoami -> POST auth /internal/admin/verify.
// It returns only {"name"}: the admin id is not needed by the browser.
func (p *Proxy) Whoami(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	status, body, ok := p.call(w, r, token, upstreamCall{
		route: "whoami", method: http.MethodPost, path: "/internal/admin/verify",
	})
	if !ok {
		return
	}
	if status != http.StatusOK {
		relay(w, status, body)
		return
	}
	var v struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &v); err != nil || strings.TrimSpace(v.Name) == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": v.Name})
}

// Accounts handles GET /api/accounts -> GET auth /internal/admin/accounts.
// Only search, status, page and limit are read from the query, each validated.
func (p *Proxy) Accounts(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	in := r.URL.Query()
	q := url.Values{}

	search, ok := cleanSearch(in.Get("search"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if search != "" {
		q.Set("search", search)
	}
	if status := strings.TrimSpace(in.Get("status")); status != "" {
		if !statusFilters[status] {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		q.Set("status", status)
	}
	if !copyBoundedInt(q, in, "page", 1, maxPage) || !copyBoundedInt(q, in, "limit", 1, maxLimit) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if rawIDs := strings.TrimSpace(in.Get("ids")); rawIDs != "" {
		parts := strings.Split(rawIDs, ",")
		if len(parts) > 100 {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		cleaned := make([]string, 0, len(parts))
		seen := make(map[string]bool, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if !idPattern.MatchString(trimmed) {
				writeError(w, http.StatusBadRequest, "bad_request")
				return
			}
			if !seen[trimmed] {
				seen[trimmed] = true
				cleaned = append(cleaned, trimmed)
			}
		}
		if len(cleaned) == 0 {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		q.Set("ids", strings.Join(cleaned, ","))
	}
	p.relayCall(w, r, token, upstreamCall{
		route: "accounts.list", method: http.MethodGet, path: "/internal/admin/accounts", query: q,
	})
}

// Suspend handles POST /api/accounts/suspend {id, reason}
// -> POST auth /internal/admin/accounts/{id}/suspend {reason}.
func (p *Proxy) Suspend(w http.ResponseWriter, r *http.Request) {
	p.accountAction(w, r, "accounts.suspend", http.MethodPost, "/suspend", true)
}

// Reactivate handles POST /api/accounts/reactivate {id}
// -> POST auth /internal/admin/accounts/{id}/reactivate.
func (p *Proxy) Reactivate(w http.ResponseWriter, r *http.Request) {
	p.accountAction(w, r, "accounts.reactivate", http.MethodPost, "/reactivate", false)
}

// Delete handles POST /api/accounts/delete {id, reason}
// -> DELETE auth /internal/admin/accounts/{id} {reason}.
func (p *Proxy) Delete(w http.ResponseWriter, r *http.Request) {
	p.accountAction(w, r, "accounts.delete", http.MethodDelete, "", true)
}

// Audit handles GET /api/audit -> GET auth /internal/admin/audit-log, or
// -> GET academy /internal/admin/audit-log when source=academy. The two logs
// are never merged: merged pagination over two sources is wrong without a
// shared cursor, so the page shows one source at a time.
func (p *Proxy) Audit(w http.ResponseWriter, r *http.Request) {
	token, ok := p.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	in := r.URL.Query()
	q := url.Values{}
	if !copyBoundedInt(q, in, "page", 1, maxPage) || !copyBoundedInt(q, in, "limit", 1, maxLimit) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	switch source := strings.TrimSpace(in.Get("source")); source {
	case "", "auth":
		p.relayCall(w, r, token, upstreamCall{
			route: "audit.list", method: http.MethodGet, path: "/internal/admin/audit-log", query: q,
		})
	case "academy":
		p.relayAcademy(w, r, token, upstreamCall{
			route: "audit.academy", method: http.MethodGet, path: "/internal/admin/audit-log", query: q,
		})
	default:
		writeError(w, http.StatusBadRequest, "bad_request")
	}
}

// accountAction implements suspend, reactivate and delete. withReason selects
// whether a reason (1-1000 characters) is required and forwarded.
func (p *Proxy) accountAction(w http.ResponseWriter, r *http.Request, route, upstreamMethod, suffix string, withReason bool) {
	token, ok := p.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	var in accountRequest
	if !decodeBody(w, r, &in, withReason) {
		return
	}
	if !idPattern.MatchString(in.ID) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	call := upstreamCall{
		route: route, method: upstreamMethod,
		path:     "/internal/admin/accounts/" + in.ID + suffix,
		targetID: in.ID,
	}
	if withReason {
		raw, ok := in.reason()
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		reason, ok := CleanReason(raw)
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		b, err := json.Marshal(struct {
			Reason string `json:"reason"`
		}{reason})
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		call.body = b
	}
	p.relayCall(w, r, token, call)
}

// ---------------------------------------------------------------------------
// Request preparation
// ---------------------------------------------------------------------------

// begin enforces the method and the presence of a well-formed admin token.
// A missing token is refused here, before any upstream call: auth-service
// counts an empty token as a failed attempt against the client IP lockout.
func (p *Proxy) begin(w http.ResponseWriter, r *http.Request, method string) (token string, ok bool) {
	if r.Method != method {
		w.Header().Set("Allow", method)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return "", false
	}
	token = r.Header.Get(AdminTokenHeader)
	if !validToken(token) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return "", false
	}
	return token, true
}

// validToken accepts printable ASCII only, so the value is safe to place in a
// header and cannot carry CR/LF. Admin tokens are base64url.
func validToken(s string) bool {
	if s == "" || len(s) > maxTokenLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// accountRequest is the body of the three account actions. Reason keeps the
// raw JSON so "key absent", "null" and "a string" can be told apart.
type accountRequest struct {
	ID     string          `json:"id"`
	Reason json.RawMessage `json:"reason"`
}

// reason returns the reason as a string; ok is false when the key is absent,
// null, or not a string.
func (a accountRequest) reason() (string, bool) {
	raw := bytes.TrimSpace(a.Reason)
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// decodeBody reads a size-capped, strict JSON object. Invalid UTF-8, unknown
// fields and trailing data are refused, and a reason is refused where none is
// expected.
func decodeBody(w http.ResponseWriter, r *http.Request, dst *accountRequest, withReason bool) bool {
	if !decodeStrict(w, r, dst) {
		return false
	}
	if !withReason && dst.Reason != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	return true
}

// decodeStrict reads a size-capped JSON value with unknown fields and
// trailing data refused.
func decodeStrict(w http.ResponseWriter, r *http.Request, dst any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil || !utf8.Valid(raw) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	return true
}

// CleanReason replaces every control character (CR and LF included) with a
// space, trims, and enforces 1-1000 characters. It reports false for invalid
// UTF-8, an empty result, or an over-long reason.
func CleanReason(raw string) (string, bool) {
	s, ok := cleanText(raw)
	if !ok {
		return "", false
	}
	n := utf8.RuneCountInString(s)
	if n < 1 || n > maxReasonRunes {
		return "", false
	}
	return s, true
}

// cleanSearch applies the same cleaning to the search text; empty is allowed,
// more than 100 characters is not (auth-service refuses it too).
func cleanSearch(raw string) (string, bool) {
	s, ok := cleanText(raw)
	if !ok || utf8.RuneCountInString(s) > maxSearchRunes {
		return "", false
	}
	return s, true
}

func cleanText(raw string) (string, bool) {
	if !utf8.ValidString(raw) {
		return "", false
	}
	s := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, raw)
	return strings.TrimSpace(s), true
}

// copyBoundedInt copies an optional integer query parameter after checking
// min <= value <= max. An absent parameter is fine; a malformed one is not.
func copyBoundedInt(dst, src url.Values, name string, min, max int) bool {
	raw := strings.TrimSpace(src.Get(name))
	if raw == "" {
		return true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		return false
	}
	dst.Set(name, strconv.Itoa(n))
	return true
}

// ---------------------------------------------------------------------------
// Client address
// ---------------------------------------------------------------------------

// ClientIP returns the address of the browser as best it can be known:
// the peer address, replaced by the right-most X-Forwarded-For entry that is
// not itself a trusted proxy, but only when the peer is a trusted proxy.
// Anything malformed falls back to the peer address. It returns "" only if
// the peer address cannot be parsed.
func (p *Proxy) ClientIP(r *http.Request) string {
	peer, ok := parseHostAddr(r.RemoteAddr)
	if !ok {
		return ""
	}
	if !p.isTrusted(peer) {
		return peer.String()
	}
	var entries []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		entries = append(entries, strings.Split(v, ",")...)
	}
	for i := len(entries) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(entries[i]))
		if err != nil {
			return peer.String()
		}
		addr = addr.Unmap().WithZone("")
		if p.isTrusted(addr) {
			continue
		}
		return addr.String()
	}
	return peer.String()
}

func (p *Proxy) isTrusted(a netip.Addr) bool {
	for _, prefix := range p.trusted {
		if prefix.Contains(a) {
			return true
		}
	}
	return false
}

func parseHostAddr(remote string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap().WithZone(""), true
}

// ---------------------------------------------------------------------------
// Upstream call
// ---------------------------------------------------------------------------

type upstreamCall struct {
	route    string // log label
	method   string
	path     string // fixed or built from a validated id
	query    url.Values
	body     []byte
	targetID string // validated id, logged for account and catalog actions
	academy  bool   // send to ACADEMY_ADMIN_URL instead of the auth listener
}

// relayCall performs the call and relays a usable answer to the browser.
func (p *Proxy) relayCall(w http.ResponseWriter, r *http.Request, token string, c upstreamCall) {
	status, body, ok := p.call(w, r, token, c)
	if !ok {
		return
	}
	relay(w, status, body)
}

// call sends the request and returns the upstream status and JSON body. When
// the upstream is unreachable, slow, answers 5xx or an unusable body, it
// writes a safe 503 itself and returns ok=false. The upstream request carries
// only the headers set here.
func (p *Proxy) call(w http.ResponseWriter, r *http.Request, token string, c upstreamCall) (int, []byte, bool) {
	target := p.authURL + c.path
	if c.academy {
		target = p.academyURL + c.path
	}
	if len(c.query) > 0 {
		target += "?" + c.query.Encode()
	}
	ctx, cancel := context.WithTimeout(r.Context(), p.timeout)
	defer cancel()

	var body io.Reader
	if c.body != nil {
		body = bytes.NewReader(c.body)
	}
	// #nosec G704 //nolint:gosec -- scheme and host come from validated config; the path is fixed or built from a validated UUID; the query is URL-encoded
	req, err := http.NewRequestWithContext(ctx, c.method, target, body)
	if err != nil {
		logOutcome(c, 0, "build_error")
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return 0, nil, false
	}
	req.Header.Set("Accept", "application/json")
	if c.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(InternalTokenHeader, p.internalToken)
	req.Header.Set(AdminTokenHeader, token)
	if ip := p.ClientIP(r); ip != "" {
		req.Header.Set(ClientIPHeader, ip)
	}

	// #nosec G704 //nolint:gosec -- same request as above
	resp, err := p.client.Do(req)
	return p.finish(w, c, resp, err)
}

// finish turns the upstream answer (or transport error) into the status and
// JSON body to relay. When the upstream is unreachable, slow, answers 5xx or
// an unusable body, it writes a safe 503 itself and returns ok=false.
func (p *Proxy) finish(w http.ResponseWriter, c upstreamCall, resp *http.Response, err error) (int, []byte, bool) {
	if err != nil {
		reason := "error"
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			reason = "timeout"
		}
		logOutcome(c, 0, reason)
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return 0, nil, false
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil || len(data) > maxResponseBody {
		logOutcome(c, resp.StatusCode, "bad_body")
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return 0, nil, false
	}
	logOutcome(c, resp.StatusCode, "")

	// Relay 2xx and 4xx; anything else (5xx, 1xx, 3xx) is "unavailable".
	if resp.StatusCode < 200 || resp.StatusCode >= 500 || (resp.StatusCode >= 300 && resp.StatusCode < 400) {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return 0, nil, false
	}
	if retry := resp.Header.Get("Retry-After"); retry != "" && isDigits(retry) {
		w.Header().Set("Retry-After", retry)
	}
	if !json.Valid(data) {
		if resp.StatusCode >= 400 {
			// Keep the status the browser maps to a message, drop the unusable body.
			return resp.StatusCode, []byte(`{"error":"request failed","code":"upstream_error"}`), true
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return 0, nil, false
	}
	return resp.StatusCode, data, true
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func isDigits(s string) bool {
	if s == "" || len(s) > 6 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// logOutcome records the route, the validated target id and status codes only.
// It never logs tokens, reasons, queries, bodies, addresses or error text.
func logOutcome(c upstreamCall, status int, failure string) {
	target := ""
	if c.targetID != "" {
		target = " target=" + c.targetID
	}
	if failure != "" {
		log.Printf("[ADMIN-CONSOLE] route=%s%s upstream_failure=%s upstream_status=%d", c.route, target, failure, status)
		return
	}
	log.Printf("[ADMIN-CONSOLE] route=%s%s upstream_status=%d", c.route, target, status)
}

// ---------------------------------------------------------------------------
// Responses
// ---------------------------------------------------------------------------

func relay(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// #nosec G705 //nolint:gosec -- body is JSON that passed json.Valid (or a fixed safe error), sent as application/json with nosniff and a strict CSP; it is never HTML
	_, _ = w.Write(body)
}

// writeJSON marshals small string maps, which cannot fail to encode.
func writeJSON(w http.ResponseWriter, status int, v map[string]string) {
	b, _ := json.Marshal(v)
	relay(w, status, b)
}

// writeError emits the safe error body: a short code and a generic message,
// never upstream text.
func writeError(w http.ResponseWriter, status int, code string) {
	msg := map[string]string{
		"unauthorized":       "unauthorized",
		"bad_request":        "bad request",
		"method_not_allowed": "method not allowed",
		"not_found":          "not found",
		"unavailable":        "service unavailable",
		"file_too_large":     "file too large",
		"invalid_pdf":        "file is not a PDF",
	}[code]
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// WriteNotFound writes the safe 404 body (used by the server for unknown paths).
func WriteNotFound(w http.ResponseWriter) { writeError(w, http.StatusNotFound, "not_found") }
