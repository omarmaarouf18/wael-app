package proxy

import (
	"net/http"
	"strings"
	"testing"
)

func TestBroadcastsPreview_ForwardsDryRun(t *testing.T) {
	up := newUpstream(t, http.StatusOK, `{"audience":"all","recipient_count":3}`)
	p := newProxy(t, up)
	rec := do(p.BroadcastsPreview, "POST", "/api/broadcasts/preview", `{"audience":"all"}`, map[string]string{AdminTokenHeader: testToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	calls := up.calls()
	if len(calls) != 1 || calls[0].Method != "POST" || calls[0].Path != "/internal/admin/broadcasts/preview" {
		t.Fatalf("upstream calls = %+v", calls)
	}
	if !strings.Contains(calls[0].Body, `"audience":"all"`) {
		t.Fatalf("upstream body = %s", calls[0].Body)
	}
	if !strings.Contains(rec.Body.String(), `"recipient_count":3`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestBroadcastsPreview_RejectsBadAudienceWithoutUpstream(t *testing.T) {
	up := newUpstream(t, http.StatusOK, `{}`)
	p := newProxy(t, up)
	for _, body := range []string{`{"audience":"everyone"}`, `{"audience":"subject:bad id!"}`, `{"audience":""}`, `{}`} {
		rec := do(p.BroadcastsPreview, "POST", "/api/broadcasts/preview", body, map[string]string{AdminTokenHeader: testToken})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d (%s)", body, rec.Code, rec.Body.String())
		}
	}
	if len(up.calls()) != 0 {
		t.Fatalf("no upstream call expected, got %+v", up.calls())
	}
}

func TestBroadcastsCreate_RebuildsBody(t *testing.T) {
	up := newUpstream(t, http.StatusCreated, `{"id":"bc-1","status":"queued","total":2,"delivered":0,"failed":0}`)
	p := newProxy(t, up)
	rec := do(p.BroadcastsCreate, "POST", "/api/broadcasts/create",
		`{"audience":"subject:sub-1","title_ar":"ع","title_en":"T","body_ar":"ن","body_en":"B","extra":"drop"}`,
		map[string]string{AdminTokenHeader: testToken})
	_ = rec
	// Unknown fields are refused before any upstream call.
	if len(up.calls()) != 0 {
		t.Fatalf("unknown fields must be refused, got %+v", up.calls())
	}
	rec = do(p.BroadcastsCreate, "POST", "/api/broadcasts/create",
		`{"audience":"subject:sub-1","title_ar":"ع","title_en":"T","body_ar":"ن","body_en":"B"}`,
		map[string]string{AdminTokenHeader: testToken})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	calls := up.calls()
	if len(calls) != 1 || calls[0].Path != "/internal/admin/broadcasts" {
		t.Fatalf("upstream calls = %+v", calls)
	}
	for _, want := range []string{`"audience":"subject:sub-1"`, `"title_ar":"ع"`, `"body_en":"B"`} {
		if !strings.Contains(calls[0].Body, want) {
			t.Fatalf("upstream body = %s, want %s", calls[0].Body, want)
		}
	}
}

func TestBroadcastsList_ForwardsPaging(t *testing.T) {
	up := newUpstream(t, http.StatusOK, `{"items":[],"total":0,"page":1,"limit":15}`)
	p := newProxy(t, up)
	rec := do(p.BroadcastsList, "GET", "/api/broadcasts?page=2&limit=10", "", map[string]string{AdminTokenHeader: testToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	calls := up.calls()
	if len(calls) != 1 || calls[0].Path != "/internal/admin/broadcasts" || calls[0].RawQuery != "limit=10&page=2" {
		t.Fatalf("upstream calls = %+v", calls)
	}
}

func TestBroadcastsRetry_ValidatesID(t *testing.T) {
	up := newUpstream(t, http.StatusOK, `{"id":"bc-1","status":"sending","retried":1,"total":2,"delivered":1,"failed":1}`)
	p := newProxy(t, up)
	id := "0f8fad5b-d9cb-469f-a165-70867728950e"
	rec := do(p.BroadcastsRetry, "POST", "/api/broadcasts/retry", `{"id":"`+id+`"}`, map[string]string{AdminTokenHeader: testToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	calls := up.calls()
	if len(calls) != 1 || calls[0].Path != "/internal/admin/broadcasts/"+id+"/retry" {
		t.Fatalf("upstream calls = %+v", calls)
	}
	for _, body := range []string{`{"id":"../evil"}`, `{"id":"a/b"}`, `{"id":""}`, `{}`} {
		rec := do(p.BroadcastsRetry, "POST", "/api/broadcasts/retry", body, map[string]string{AdminTokenHeader: testToken})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d", body, rec.Code)
		}
	}
}

func TestBroadcasts_RelayUpstreamErrors(t *testing.T) {
	up := newUpstream(t, http.StatusTooManyRequests, `{"error":"slow","code":"rate_limited"}`)
	up.header = map[string]string{"Retry-After": "7"}
	p := newProxy(t, up)
	rec := do(p.BroadcastsCreate, "POST", "/api/broadcasts/create",
		`{"audience":"all","title_ar":"ع","body_ar":"ن"}`,
		map[string]string{AdminTokenHeader: testToken})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "7" {
		t.Fatalf("Retry-After not relayed: %+v", rec.Header())
	}
	if !strings.Contains(rec.Body.String(), `"code":"rate_limited"`) {
		t.Fatalf("code not relayed: %s", rec.Body.String())
	}
}

func TestAudit_NotificationsSource(t *testing.T) {
	up := newUpstream(t, http.StatusOK, `{"items":[],"total":0,"page":1,"limit":20}`)
	p := newProxy(t, up)
	rec := do(p.Audit, "GET", "/api/audit?source=notifications&page=1&limit=20", "", map[string]string{AdminTokenHeader: testToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	calls := up.calls()
	if len(calls) != 1 || calls[0].Path != "/internal/admin/audit-log" {
		t.Fatalf("upstream calls = %+v", calls)
	}
}
