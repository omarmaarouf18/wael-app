package handlers

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/limiter"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
)

// DownloadFile handles GET /academy/subjects/{id}/files/{fileId}/download
// (SPEC Phase 5.2, Section 7 R1, R3, R6, R8, D4).
//
// Every call re-checks ownership (an unexpired, unrevoked entitlement for
// this student and subject) before the stored object is touched: nothing is
// cached and there is no signed or static URL (R3). The PDF streams through
// academy-service (R6) with Content-Disposition: attachment (sanitized
// filename), Cache-Control: private, no-store, X-Content-Type-Options:
// nosniff, Content-Type: application/pdf and Content-Length equal to the
// plaintext size. The body is written by one io.Copy from the decrypted
// reader, so a per-user watermark (SPEC Section 3 question 2) can later wrap
// that reader without changing the route or the response shape.
//
// Downloads in flight are capped (MAX_CONCURRENT_DOWNLOADS, default 3): each
// holds one plaintext copy of its file in memory. The slot is taken after the
// ownership decision, only for the part that reads the object, and a student
// has at most one download in flight. When every slot is busy, or this
// student already has a download running, the answer is 429 with
// Retry-After (the rate limiter's shape); nothing waits in a queue.
//
// Answers: unknown subject, unknown file, or a file of another subject 404;
// a subject the student does not own 403 (404 instead when the subject is
// hidden from non-owners, R8); an owned subject whose access date has passed
// 403; storage failure 503 with no detail. Rate limited by the Download tier.
func (s *Server) DownloadFile(w http.ResponseWriter, r *http.Request, subjectID, fileID string) {
	w.Header().Set("Cache-Control", "private, no-store")

	if r.Method != http.MethodGet {
		handlerutil.WriteSafeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	claims := StudentClaims(r)
	if claims == nil || claims.UserID == "" {
		handlerutil.WriteSafeError(w, r, http.StatusUnauthorized, handlerutil.ErrCodeUnauthorized, "unauthorized", nil)
		return
	}
	notFound := func() {
		handlerutil.WriteSafeError(w, r, http.StatusNotFound, "not_found", "not found", nil)
	}
	unavailable := func(err error) {
		handlerutil.WriteSafeError(w, r, http.StatusServiceUnavailable, handlerutil.ErrCodeUnavailable, "service temporarily unavailable", err)
	}
	if !validResourceID(subjectID) || !validResourceID(fileID) {
		notFound()
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	subj, err := s.Store.GetSubjectByID(dbCtx, subjectID)
	if err != nil {
		unavailable(err)
		return
	}
	if subj == nil {
		notFound()
		return
	}

	file, err := s.Store.GetFile(dbCtx, subjectID, fileID)
	if err != nil {
		unavailable(err)
		return
	}
	if file == nil {
		notFound()
		return
	}

	// R1, checked now and never cached: active, unrevoked, unexpired.
	owned, err := s.Store.HasActiveEntitlement(dbCtx, claims.UserID, subjectID)
	if err != nil {
		unavailable(err)
		return
	}
	if !owned {
		// R8: a subject hidden from non-owners hides its files too.
		lvl, err := s.Store.GetLevelByKey(dbCtx, subj.LevelKey)
		if err != nil {
			unavailable(err)
			return
		}
		if !studentCanSee(map[string]*models.Level{subj.LevelKey: lvl}, subj, false) {
			notFound()
			return
		}
		handlerutil.WriteSafeError(w, r, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	// Same rule as /play: a subject whose access date has passed is locked.
	if !subj.AccessExpiresAt.IsZero() && time.Now().After(subj.AccessExpiresAt) {
		handlerutil.WriteSafeError(w, r, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}

	// Only now is the stored object touched.
	if s.Files == nil {
		unavailable(errors.New("file storage unconfigured"))
		return
	}
	release, ok := s.acquireDownloadSlot(claims.UserID)
	if !ok {
		limiter.WriteRateLimitedResponse(w, downloadBusyRetryAfter)
		return
	}
	defer release()
	size, err := s.Files.Size(file.StorageKey)
	if err != nil {
		unavailable(err)
		return
	}
	rc, err := s.Files.OpenFile(file.StorageKey)
	if err != nil {
		unavailable(err)
		return
	}
	defer func() { _ = rc.Close() }()

	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	h.Set("Content-Disposition", contentDisposition(file))
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if n, err := s.copyWithStallDeadline(w, rc); err != nil || n != size {
		// Headers are gone; the client sees a short body against
		// Content-Length and discards it. Ids only in the log; a stalled
		// client shows as a deadline error here.
		cleanID := strings.ReplaceAll(strings.ReplaceAll(file.ID, "\r", ""), "\n", "")
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[WARN] file_download incomplete file_id=%s sent=%d of %d: %v", cleanID, n, size, err)
	}
}

// downloadChunk is the size of one write of a download.
const downloadChunk = 64 << 10

// defaultDownloadStallTimeout mirrors config.DefaultDownloadStallTimeout for
// servers built without config (tests).
const defaultDownloadStallTimeout = 30 * time.Second

func (s *Server) downloadStallTimeout() time.Duration {
	if s.DownloadStallTimeout > 0 {
		return s.DownloadStallTimeout
	}
	return defaultDownloadStallTimeout
}

// copyWithStallDeadline copies src to w in 64 KiB chunks and, before each
// chunk, moves the connection's write deadline to now + the stall timeout.
// Neither academy nor the gateway has a server WriteTimeout (by design), so
// this is what ends a download whose client stopped reading: the blocked
// write fails at the deadline, the handler returns, and the deferred release
// gives the slot back. A writer without deadline support is logged once and
// the copy continues without one. The deadline is cleared at the end so a
// kept-alive connection is not cut later.
func (s *Server) copyWithStallDeadline(w http.ResponseWriter, src io.Reader) (int64, error) {
	ctl := http.NewResponseController(w)
	timeout := s.downloadStallTimeout()
	deadlines := true
	buf := make([]byte, downloadChunk)
	var written int64
	defer func() {
		if deadlines {
			_ = ctl.SetWriteDeadline(time.Time{})
		}
	}()
	for {
		if deadlines {
			if err := ctl.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
				deadlines = false
				if errors.Is(err, http.ErrNotSupported) {
					s.stallWarnOnce.Do(func() {
						log.Printf("[WARN] file_download: response writer has no write deadline support; stalled downloads are not cut")
					})
				}
			}
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			m, werr := w.Write(buf[:n])
			written += int64(m)
			if werr != nil {
				return written, werr
			}
		}
		if errors.Is(rerr, io.EOF) {
			return written, nil
		}
		if rerr != nil {
			return written, rerr
		}
	}
}

// contentDisposition builds "attachment" with a sanitized ASCII filename and
// an RFC 5987 UTF-8 filename* (Arabic title preferred, English as fallback).
// Nothing in it comes from the uploaded file's own name.
func contentDisposition(f *models.SubjectFile) string {
	ascii := asciiFilename(f.TitleEn)
	if ascii == "" {
		ascii = asciiFilename(f.Kind)
	}
	if ascii == "" {
		ascii = "document"
	}
	v := `attachment; filename="` + ascii + `.pdf"`
	title := f.TitleAr
	if strings.TrimSpace(title) == "" {
		title = f.TitleEn
	}
	if u := unicodeFilename(title); u != "" {
		v += "; filename*=UTF-8''" + rfc5987Encode(u+".pdf")
	}
	return v
}

const maxFilenameRunes = 80

// asciiFilename keeps ASCII letters, digits, '-' and '_'; runs of anything
// else become a single '-'. At most 80 characters, no leading or trailing '-'.
func asciiFilename(s string) string {
	var b strings.Builder
	dash := false
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteRune(c)
			dash = false
		} else if b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= maxFilenameRunes {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// unicodeFilename keeps letters, digits and marks (Arabic included); runs of
// anything else (separators, quotes, controls, '.', path characters) become a
// single '-'. At most 80 runes.
func unicodeFilename(s string) string {
	var out []rune
	dash := false
	for _, c := range s {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || unicode.IsMark(c) || c == '_' {
			out = append(out, c)
			dash = false
		} else if len(out) > 0 && !dash {
			out = append(out, '-')
			dash = true
		}
		if len(out) >= maxFilenameRunes {
			break
		}
	}
	return strings.Trim(string(out), "-")
}

// rfc5987Encode percent-encodes everything outside RFC 5987 attr-char.
func rfc5987Encode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0f])
	}
	return b.String()
}

// downloadBusyRetryAfter is the Retry-After sent when every download slot is
// busy.
const downloadBusyRetryAfter = 5 * time.Second

// defaultMaxConcurrentDownloads mirrors config.DefaultMaxConcurrentDownloads
// for servers built without config (tests); the cap is never unlimited.
const defaultMaxConcurrentDownloads = 3

// acquireDownloadSlot takes a download slot for userID without waiting. It
// returns the release function, or ok=false when this student already has a
// download in flight or every slot is busy. Both are decided under one lock,
// so a refused call changes nothing. The release runs once however the
// download ends (success, client abort, stall, storage error) and removes the
// student's entry.
func (s *Server) acquireDownloadSlot(userID string) (release func(), ok bool) {
	s.downloadSlotsOnce.Do(func() {
		n := s.MaxConcurrentDownloads
		if n <= 0 {
			n = defaultMaxConcurrentDownloads
		}
		s.downloadSlots = make(chan struct{}, n)
		s.downloadUsers = make(map[string]struct{})
	})
	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()
	if _, busy := s.downloadUsers[userID]; busy {
		return nil, false
	}
	select {
	case s.downloadSlots <- struct{}{}:
	default:
		return nil, false
	}
	s.downloadUsers[userID] = struct{}{}
	var once sync.Once
	return func() {
		once.Do(func() {
			s.downloadMu.Lock()
			delete(s.downloadUsers, userID)
			s.downloadMu.Unlock()
			<-s.downloadSlots
		})
	}, true
}

// downloadsInFlight reports the students with a download in flight (tests).
func (s *Server) downloadsInFlight() int {
	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()
	return len(s.downloadUsers)
}
