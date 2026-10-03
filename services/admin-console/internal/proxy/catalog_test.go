package proxy

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testLevelID = "dip-a"

// catalogRoutes returns the table entries for the academy-backed routes.
// They join routes() (see proxy_test.go) so the generic guarantees (401
// without a call, 405, exact upstream mapping, stripped client headers, 503s,
// relayed 4xx) hold for them too; academy-vs-auth routing is proven below.
func catalogRoutes() []route {
	return []route{
		{
			name: "levels.list", handler: func(p *Proxy) http.HandlerFunc { return p.LevelsList },
			method: http.MethodGet, target: "/api/levels",
			upstreamMethod: http.MethodGet, upstreamPath: "/internal/admin/levels",
			upstreamStatus: 200, upstreamReply: `{"levels":[]}`,
		},
		{
			name: "levels.create", handler: func(p *Proxy) http.HandlerFunc { return p.LevelsCreate },
			method: http.MethodPost, target: "/api/levels/create",
			body:           `{"study_type":"diploma","name_ar":"Data"}`,
			upstreamMethod: http.MethodPost, upstreamPath: "/internal/admin/levels",
			upstreamBody:   `{"name_ar":"Data","study_type":"diploma"}`,
			upstreamStatus: 201, upstreamReply: `{"key":"dip-a"}`,
			wantStatus: 201,
		},
		{
			name: "levels.update", handler: func(p *Proxy) http.HandlerFunc { return p.LevelsUpdate },
			method: http.MethodPost, target: "/api/levels/update",
			body:           `{"id":"` + testLevelID + `","name_ar":"New","published":true}`,
			upstreamMethod: http.MethodPatch, upstreamPath: "/internal/admin/levels/" + testLevelID,
			upstreamBody:   `{"name_ar":"New","published":true}`,
			upstreamStatus: 200, upstreamReply: `{"key":"` + testLevelID + `"}`,
		},
		{
			name: "levels.delete", handler: func(p *Proxy) http.HandlerFunc { return p.LevelsDelete },
			method: http.MethodPost, target: "/api/levels/delete",
			body:           `{"id":"` + testLevelID + `"}`,
			upstreamMethod: http.MethodDelete, upstreamPath: "/internal/admin/levels/" + testLevelID,
			upstreamStatus: 200, upstreamReply: `{"status":"ok"}`,
		},
		{
			name: "subjects.list", handler: func(p *Proxy) http.HandlerFunc { return p.SubjectsList },
			method: http.MethodGet, target: "/api/subjects?level_id=" + testLevelID + "&published=true&page=2&limit=10",
			upstreamMethod: http.MethodGet, upstreamPath: "/internal/admin/subjects",
			upstreamQuery:  "level_id=" + testLevelID + "&limit=10&page=2&published=true",
			upstreamStatus: 200, upstreamReply: `{"items":[],"total":0,"page":2,"limit":10}`,
		},
		{
			name: "subjects.create", handler: func(p *Proxy) http.HandlerFunc { return p.SubjectsCreate },
			method: http.MethodPost, target: "/api/subjects/create",
			body:           `{"level_id":"` + testLevelID + `","title_ar":"Math","price":100,"access_expires_at":"2027-01-01T00:00:00Z"}`,
			upstreamMethod: http.MethodPost, upstreamPath: "/internal/admin/subjects",
			upstreamBody:   `{"access_expires_at":"2027-01-01T00:00:00Z","level_id":"` + testLevelID + `","price":100,"title_ar":"Math"}`,
			upstreamStatus: 201, upstreamReply: `{"id":"` + testID + `"}`,
			wantStatus: 201,
		},
		{
			name: "subjects.update", handler: func(p *Proxy) http.HandlerFunc { return p.SubjectsUpdate },
			method: http.MethodPost, target: "/api/subjects/update",
			body:           `{"id":"` + testID + `","title_ar":"New","term":"first"}`,
			upstreamMethod: http.MethodPatch, upstreamPath: "/internal/admin/subjects/" + testID,
			upstreamBody:   `{"term":"first","title_ar":"New"}`,
			upstreamStatus: 200, upstreamReply: `{"id":"` + testID + `"}`,
		},
		{
			name: "subjects.publish", handler: func(p *Proxy) http.HandlerFunc { return p.SubjectsPublish },
			method: http.MethodPost, target: "/api/subjects/publish",
			body:           `{"id":"` + testID + `"}`,
			upstreamMethod: http.MethodPost, upstreamPath: "/internal/admin/subjects/" + testID + "/publish",
			upstreamStatus: 200, upstreamReply: `{"id":"` + testID + `"}`,
		},
		{
			name: "subjects.unpublish", handler: func(p *Proxy) http.HandlerFunc { return p.SubjectsUnpublish },
			method: http.MethodPost, target: "/api/subjects/unpublish",
			body:           `{"id":"` + testID + `"}`,
			upstreamMethod: http.MethodPost, upstreamPath: "/internal/admin/subjects/" + testID + "/unpublish",
			upstreamStatus: 200, upstreamReply: `{"id":"` + testID + `"}`,
		},
		{
			name: "videos.list", handler: func(p *Proxy) http.HandlerFunc { return p.VideosList },
			method: http.MethodGet, target: "/api/videos?subject_id=" + testID,
			upstreamMethod: http.MethodGet, upstreamPath: "/internal/admin/subjects/" + testID + "/videos",
			upstreamStatus: 200, upstreamReply: `{"videos":[]}`,
		},
		{
			name: "videos.create", handler: func(p *Proxy) http.HandlerFunc { return p.VideosCreate },
			method: http.MethodPost, target: "/api/videos/create",
			body:           `{"subject_id":"` + testID + `","title_ar":"Intro","youtube":"dQw4w9WgXcQ"}`,
			upstreamMethod: http.MethodPost, upstreamPath: "/internal/admin/subjects/" + testID + "/videos",
			upstreamBody:   `{"title_ar":"Intro","youtube":"dQw4w9WgXcQ"}`,
			upstreamStatus: 201, upstreamReply: `{"id":"v1"}`,
			wantStatus: 201,
		},
		{
			name: "videos.update", handler: func(p *Proxy) http.HandlerFunc { return p.VideosUpdate },
			method: http.MethodPost, target: "/api/videos/update",
			body:           `{"id":"` + testID + `","duration_seconds":90}`,
			upstreamMethod: http.MethodPatch, upstreamPath: "/internal/admin/videos/" + testID,
			upstreamBody:   `{"duration_seconds":90}`,
			upstreamStatus: 200, upstreamReply: `{"id":"` + testID + `"}`,
		},
		{
			name: "videos.reorder", handler: func(p *Proxy) http.HandlerFunc { return p.VideosReorder },
			method: http.MethodPost, target: "/api/videos/reorder",
			body:           `{"subject_id":"` + testID + `","video_ids":["a1","b2"]}`,
			upstreamMethod: http.MethodPost, upstreamPath: "/internal/admin/subjects/" + testID + "/videos/reorder",
			upstreamBody:   `{"video_ids":["a1","b2"]}`,
			upstreamStatus: 200, upstreamReply: `{"videos":[]}`,
		},
		{
			name: "videos.delete", handler: func(p *Proxy) http.HandlerFunc { return p.VideosDelete },
			method: http.MethodPost, target: "/api/videos/delete",
			body:           `{"id":"` + testID + `"}`,
			upstreamMethod: http.MethodDelete, upstreamPath: "/internal/admin/videos/" + testID,
			upstreamStatus: 200, upstreamReply: `{"status":"ok"}`,
		},
	}
}

// ---------------------------------------------------------------------------
// Academy vs auth routing
// ---------------------------------------------------------------------------

// newSplitProxy builds a Proxy whose auth and academy upstreams are different
// fakes, so tests can prove which listener a route reaches.
func newSplitProxy(t *testing.T, authUp, academyUp *fakeUpstream) *Proxy {
	t.Helper()
	p, err := New(Options{
		InternalToken: testInternal,
		AuthURL:       authUp.srv.URL,
		AcademyURL:    academyUp.srv.URL,
		Timeout:       2 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func TestCatalog_RoutesReachAcademyNotAuth(t *testing.T) {
	for _, rt := range catalogRoutes() {
		t.Run(rt.name, func(t *testing.T) {
			authUp := newUpstream(t, 200, `{"items":[]}`)
			academyUp := newUpstream(t, rt.upstreamStatus, rt.upstreamReply)
			p := newSplitProxy(t, authUp, academyUp)
			w := do(rt.handler(p), rt.method, rt.target, rt.body, withToken())
			if w.Code != http.StatusOK && w.Code != http.StatusCreated {
				t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
			}
			if n := len(authUp.calls()); n != 0 {
				t.Fatalf("auth upstream was called %d times", n)
			}
			if n := len(academyUp.calls()); n != 1 {
				t.Fatalf("academy upstream calls = %d, want 1", n)
			}
		})
	}
}

func TestCatalog_NoRouteTouchesFilesRequestsOrEntitlements(t *testing.T) {
	authUp := newUpstream(t, 200, `{"items":[]}`)
	academyUp := newUpstream(t, 200, `{"items":[],"levels":[],"videos":[]}`)
	p := newSplitProxy(t, authUp, academyUp)
	for _, rt := range catalogRoutes() {
		do(rt.handler(p), rt.method, rt.target, rt.body, withToken())
	}
	for _, c := range academyUp.calls() {
		for _, forbidden := range []string{"files", "requests", "entitlements"} {
			if strings.Contains(c.Path, forbidden) {
				t.Fatalf("route reached academy %s", c.Path)
			}
		}
		if !strings.HasPrefix(c.Path, "/internal/admin/") {
			t.Fatalf("route left /internal/admin/: %s", c.Path)
		}
	}
}

func TestAudit_SourceAcademyGoesToAcademy(t *testing.T) {
	authUp := newUpstream(t, 200, `{"items":[]}`)
	academyUp := newUpstream(t, 200, `{"items":[],"total":0}`)
	p := newSplitProxy(t, authUp, academyUp)
	w := do(p.Audit, http.MethodGet, "/api/audit?source=academy&page=1&limit=20", "", withToken())
	if w.Code != 200 {
		t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
	}
	if n := len(authUp.calls()); n != 0 {
		t.Fatalf("auth upstream was called %d times", n)
	}
	calls := academyUp.calls()
	if len(calls) != 1 {
		t.Fatalf("academy calls = %d, want 1", len(calls))
	}
	if calls[0].Path != "/internal/admin/audit-log" || calls[0].RawQuery != "limit=20&page=1" {
		t.Fatalf("academy got %s?%s", calls[0].Path, calls[0].RawQuery)
	}
}

func TestAudit_SourceAuthIsDefaultAndBadSourceIs400(t *testing.T) {
	for _, target := range []string{"/api/audit?page=1&limit=5", "/api/audit?source=auth&page=1&limit=5"} {
		authUp := newUpstream(t, 200, `{"items":[]}`)
		academyUp := newUpstream(t, 200, `{"items":[]}`)
		p := newSplitProxy(t, authUp, academyUp)
		w := do(p.Audit, http.MethodGet, target, "", withToken())
		if w.Code != 200 {
			t.Fatalf("%s: status = %d", target, w.Code)
		}
		if n := len(authUp.calls()); n != 1 {
			t.Fatalf("%s: auth calls = %d, want 1", target, n)
		}
		if n := len(academyUp.calls()); n != 0 {
			t.Fatalf("%s: academy was called", target)
		}
	}
	for _, target := range []string{"/api/audit?source=all", "/api/audit?source=files", "/api/audit?source=Academy", "/api/audit?source=authx"} {
		authUp := newUpstream(t, 200, `{"items":[]}`)
		academyUp := newUpstream(t, 200, `{"items":[]}`)
		p := newSplitProxy(t, authUp, academyUp)
		w := do(p.Audit, http.MethodGet, target, "", withToken())
		assertSafeError(t, w, http.StatusBadRequest, "bad_request")
		if len(authUp.calls())+len(academyUp.calls()) != 0 {
			t.Fatalf("%s: upstream was called", target)
		}
	}
}

func init() {
	// Catalog entries join routes() via proxy_test.go so the generic table
	// tests cover them; academy-vs-auth routing is proven below.
}

// ---------------------------------------------------------------------------
// Local validation: bad input is 400 with no upstream call
// ---------------------------------------------------------------------------

func catalogBad(t *testing.T, handler func(*Proxy) http.HandlerFunc, method, target, body string) {
	t.Helper()
	authUp := newUpstream(t, 200, `{"items":[]}`)
	academyUp := newUpstream(t, 200, `{"items":[]}`)
	p := newSplitProxy(t, authUp, academyUp)
	w := do(handler(p), method, target, body, withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")
	if n := len(authUp.calls()) + len(academyUp.calls()); n != 0 {
		t.Fatalf("%s %s %q: upstream was called", method, target, body)
	}
}

func TestLevels_BadInputIs400(t *testing.T) {
	long := strings.Repeat("س", maxCatalogNameRunes+1)
	for name, body := range map[string]string{
		"missing study_type":   `{"name_ar":"x"}`,
		"bachelor study_type":  `{"study_type":"bachelor","name_ar":"x"}`,
		"empty study_type":     `{"study_type":"","name_ar":"x"}`,
		"missing name":         `{"study_type":"diploma"}`,
		"blank name":           `{"study_type":"diploma","name_ar":"  "}`,
		"name too long":        `{"study_type":"diploma","name_ar":"` + long + `"}`,
		"name_en too long":     `{"study_type":"diploma","name_ar":"x","name_en":"` + long + `"}`,
		"id present on create": `{"study_type":"diploma","name_ar":"x","id":"` + testLevelID + `"}`,
		"unknown field":        `{"study_type":"diploma","name_ar":"x","key":"y"}`,
		"trailing data":        `{"study_type":"diploma","name_ar":"x"} {}`,
		"empty body":           ``,
		"array body":           `[]`,
		"bad utf8":             "{\"study_type\":\"diploma\",\"name_ar\":\"\xff\"}",
		"order wrong type":     `{"study_type":"diploma","name_ar":"x","order":"1"}`,
		"published wrong type": `{"study_type":"diploma","name_ar":"x","published":"yes"}`,
	} {
		t.Run("create/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.LevelsCreate }, http.MethodPost, "/api/levels/create", body)
		})
	}
	for name, body := range map[string]string{
		"missing id":       `{"name_ar":"x"}`,
		"empty id":         `{"id":"","name_ar":"x"}`,
		"slash id":         `{"id":"a/b","name_ar":"x"}`,
		"space id":         `{"id":"a b","name_ar":"x"}`,
		"id too long":      `{"id":"` + strings.Repeat("a", 101) + `","name_ar":"x"}`,
		"blank name":       `{"id":"` + testLevelID + `","name_ar":"  "}`,
		"name too long":    `{"id":"` + testLevelID + `","name_ar":"` + long + `"}`,
		"bad study_type":   `{"id":"` + testLevelID + `","study_type":"x"}`,
		"blank study_type": `{"id":"` + testLevelID + `","study_type":" "}`,
		"unknown field":    `{"id":"` + testLevelID + `","key":"y"}`,
	} {
		t.Run("update/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.LevelsUpdate }, http.MethodPost, "/api/levels/update", body)
		})
	}
	for name, body := range map[string]string{
		"missing id":  `{}`,
		"empty id":    `{"id":""}`,
		"slash id":    `{"id":"../x"}`,
		"extra field": `{"id":"` + testLevelID + `","reason":"x"}`,
	} {
		t.Run("delete/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.LevelsDelete }, http.MethodPost, "/api/levels/delete", body)
		})
	}
}

func TestSubjects_BadInputIs400(t *testing.T) {
	long := strings.Repeat("س", maxCatalogNameRunes+1)
	longDesc := strings.Repeat("س", maxCatalogDescRunes+1)
	for name, target := range map[string]string{
		"bad level_id":  "/api/subjects?level_id=../x",
		"bad published": "/api/subjects?published=yes",
		"published 2":   "/api/subjects?published=2",
		"page zero":     "/api/subjects?page=0",
		"limit 101":     "/api/subjects?limit=101",
		"limit text":    "/api/subjects?limit=x",
	} {
		t.Run("list/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.SubjectsList }, http.MethodGet, target, "")
		})
	}
	for name, body := range map[string]string{
		"missing level":    `{"title_ar":"Math","access_expires_at":"2027-01-01T00:00:00Z"}`,
		"bad level":        `{"level_id":"a/b","title_ar":"Math","access_expires_at":"2027-01-01T00:00:00Z"}`,
		"missing title":    `{"level_id":"` + testLevelID + `","access_expires_at":"2027-01-01T00:00:00Z"}`,
		"blank title":      `{"level_id":"` + testLevelID + `","title_ar":" ","access_expires_at":"2027-01-01T00:00:00Z"}`,
		"title too long":   `{"level_id":"` + testLevelID + `","title_ar":"` + long + `","access_expires_at":"2027-01-01T00:00:00Z"}`,
		"desc too long":    `{"level_id":"` + testLevelID + `","title_ar":"x","description_ar":"` + longDesc + `","access_expires_at":"2027-01-01T00:00:00Z"}`,
		"bad term":         `{"level_id":"` + testLevelID + `","title_ar":"x","term":"third","access_expires_at":"2027-01-01T00:00:00Z"}`,
		"negative price":   `{"level_id":"` + testLevelID + `","title_ar":"x","price":-5,"access_expires_at":"2027-01-01T00:00:00Z"}`,
		"float price":      `{"level_id":"` + testLevelID + `","title_ar":"x","price":1.5,"access_expires_at":"2027-01-01T00:00:00Z"}`,
		"string price":     `{"level_id":"` + testLevelID + `","title_ar":"x","price":"100","access_expires_at":"2027-01-01T00:00:00Z"}`,
		"missing expires":  `{"level_id":"` + testLevelID + `","title_ar":"x"}`,
		"bad expires":      `{"level_id":"` + testLevelID + `","title_ar":"x","access_expires_at":"tomorrow"}`,
		"date not rfc3339": `{"level_id":"` + testLevelID + `","title_ar":"x","access_expires_at":"2027-01-01"}`,
		"id present":       `{"id":"` + testID + `","level_id":"` + testLevelID + `","title_ar":"x","access_expires_at":"2027-01-01T00:00:00Z"}`,
		"unknown field":    `{"level_id":"` + testLevelID + `","title_ar":"x","access_expires_at":"2027-01-01T00:00:00Z","color":"red"}`,
	} {
		t.Run("create/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.SubjectsCreate }, http.MethodPost, "/api/subjects/create", body)
		})
	}
	for name, body := range map[string]string{
		"missing id":     `{"title_ar":"x"}`,
		"slash id":       `{"id":"a/b","title_ar":"x"}`,
		"bad level move": `{"id":"` + testID + `","level_id":""}`,
		"bad term":       `{"id":"` + testID + `","term":"3"}`,
		"negative price": `{"id":"` + testID + `","price":-1}`,
		"bad expires":    `{"id":"` + testID + `","access_expires_at":"2027-13-99T99:99:99Z"}`,
		"unknown field":  `{"id":"` + testID + `","videos":3}`,
	} {
		t.Run("update/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.SubjectsUpdate }, http.MethodPost, "/api/subjects/update", body)
		})
	}
	for handlerName, handler := range map[string]func(*Proxy) http.HandlerFunc{
		"publish":   func(p *Proxy) http.HandlerFunc { return p.SubjectsPublish },
		"unpublish": func(p *Proxy) http.HandlerFunc { return p.SubjectsUnpublish },
	} {
		for name, body := range map[string]string{
			"missing id":  `{}`,
			"slash id":    `{"id":"a/b"}`,
			"extra field": `{"id":"` + testID + `","force":true}`,
		} {
			t.Run(handlerName+"/"+name, func(t *testing.T) {
				catalogBad(t, handler, http.MethodPost, "/api/subjects/"+handlerName, body)
			})
		}
	}
}

func TestVideos_BadInputIs400(t *testing.T) {
	long := strings.Repeat("س", maxCatalogNameRunes+1)
	for name, target := range map[string]string{
		"missing subject": "/api/videos",
		"blank subject":   "/api/videos?subject_id=++",
		"slash subject":   "/api/videos?subject_id=a/b",
	} {
		t.Run("list/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.VideosList }, http.MethodGet, target, "")
		})
	}
	for name, body := range map[string]string{
		"missing subject":  `{"title_ar":"x","youtube":"dQw4w9WgXcQ"}`,
		"slash subject":    `{"subject_id":"a/b","title_ar":"x","youtube":"dQw4w9WgXcQ"}`,
		"blank title":      `{"subject_id":"` + testID + `","title_ar":" ","youtube":"dQw4w9WgXcQ"}`,
		"title too long":   `{"subject_id":"` + testID + `","title_ar":"` + long + `","youtube":"dQw4w9WgXcQ"}`,
		"missing youtube":  `{"subject_id":"` + testID + `","title_ar":"x"}`,
		"blank youtube":    `{"subject_id":"` + testID + `","title_ar":"x","youtube":"  "}`,
		"negative seconds": `{"subject_id":"` + testID + `","title_ar":"x","youtube":"dQw4w9WgXcQ","duration_seconds":-1}`,
		"unknown field":    `{"subject_id":"` + testID + `","title_ar":"x","youtube":"dQw4w9WgXcQ","id":"v1"}`,
	} {
		t.Run("create/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.VideosCreate }, http.MethodPost, "/api/videos/create", body)
		})
	}
	for name, body := range map[string]string{
		"missing id":       `{"title_ar":"x"}`,
		"slash id":         `{"id":"a/b"}`,
		"blank title":      `{"id":"` + testID + `","title_ar":""}`,
		"blank youtube":    `{"id":"` + testID + `","youtube":""}`,
		"negative seconds": `{"id":"` + testID + `","duration_seconds":-30}`,
		"unknown field":    `{"id":"` + testID + `","subject_id":"` + testID + `"}`,
	} {
		t.Run("update/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.VideosUpdate }, http.MethodPost, "/api/videos/update", body)
		})
	}
	for name, body := range map[string]string{
		"missing subject": `{"video_ids":["a1"]}`,
		"missing list":    `{"subject_id":"` + testID + `"}`,
		"null list":       `{"subject_id":"` + testID + `","video_ids":null}`,
		"not an array":    `{"subject_id":"` + testID + `","video_ids":"a1"}`,
		"duplicate":       `{"subject_id":"` + testID + `","video_ids":["a1","a1"]}`,
		"bad id in list":  `{"subject_id":"` + testID + `","video_ids":["a1","a/b"]}`,
	} {
		t.Run("reorder/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.VideosReorder }, http.MethodPost, "/api/videos/reorder", body)
		})
	}
	t.Run("reorder/too many", func(t *testing.T) {
		ids := make([]string, 0, maxVideoIDs+1)
		for i := 0; i <= maxVideoIDs; i++ {
			ids = append(ids, "video-"+strconv.Itoa(10000+i))
		}
		b, _ := json.Marshal(map[string]any{"subject_id": testID, "video_ids": ids})
		catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.VideosReorder }, http.MethodPost, "/api/videos/reorder", string(b))
	})
	for name, body := range map[string]string{
		"missing id":   `{}`,
		"slash id":     `{"id":"a/b"}`,
		"force string": `{"id":"` + testID + `","force":"yes"}`,
		"force number": `{"id":"` + testID + `","force":1}`,
	} {
		t.Run("delete/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.VideosDelete }, http.MethodPost, "/api/videos/delete", body)
		})
	}
}

// ---------------------------------------------------------------------------
// Exact upstream mappings
// ---------------------------------------------------------------------------

func TestVideosDelete_ForceFlagMappingIsExact(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantQuery string
	}{
		{"absent", `{"id":"` + testID + `"}`, ""},
		{"false", `{"id":"` + testID + `","force":false}`, ""},
		{"true", `{"id":"` + testID + `","force":true}`, "force=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authUp := newUpstream(t, 200, `{}`)
			academyUp := newUpstream(t, 200, `{"status":"ok"}`)
			p := newSplitProxy(t, authUp, academyUp)
			w := do(p.VideosDelete, http.MethodPost, "/api/videos/delete", tc.body, withToken())
			if w.Code != 200 {
				t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
			}
			calls := academyUp.calls()
			if len(calls) != 1 {
				t.Fatalf("academy calls = %d", len(calls))
			}
			if calls[0].Method != http.MethodDelete || calls[0].Path != "/internal/admin/videos/"+testID || calls[0].RawQuery != tc.wantQuery {
				t.Fatalf("upstream got %s %s?%s", calls[0].Method, calls[0].Path, calls[0].RawQuery)
			}
		})
	}
}

func TestSubjectsCreate_OptionalFieldsForwardedOnlyWhenPresent(t *testing.T) {
	authUp := newUpstream(t, 200, `{}`)
	academyUp := newUpstream(t, 201, `{"id":"s1"}`)
	p := newSplitProxy(t, authUp, academyUp)
	body := `{"level_id":"` + testLevelID + `","title_ar":"Math","title_en":"","description_ar":"وصف","term":"","price":0,"access_expires_at":"2027-06-01T21:59:59Z","order":3,"published":false}`
	w := do(p.SubjectsCreate, http.MethodPost, "/api/subjects/create", body, withToken())
	if w.Code != 201 {
		t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(academyUp.calls()[0].Body), &sent); err != nil {
		t.Fatalf("upstream body not JSON: %v", err)
	}
	want := map[string]any{
		"level_id": "dip-a", "title_ar": "Math", "description_ar": "وصف",
		"term": "", "price": float64(0), "access_expires_at": "2027-06-01T21:59:59Z",
		"order": float64(3), "published": false,
	}
	if len(sent) != len(want) {
		t.Fatalf("upstream body keys = %v, want exactly %v", sent, want)
	}
	for k, v := range want {
		if sent[k] != v {
			t.Fatalf("upstream[%s] = %v, want %v", k, sent[k], v)
		}
	}
}

func TestLevelsCreate_NameEnBlankIsOmitted(t *testing.T) {
	authUp := newUpstream(t, 200, `{}`)
	academyUp := newUpstream(t, 201, `{"key":"diploma-x"}`)
	p := newSplitProxy(t, authUp, academyUp)
	w := do(p.LevelsCreate, http.MethodPost, "/api/levels/create", `{"study_type":"diploma","name_ar":"دبلومة","name_en":"  "}`, withToken())
	if w.Code != 201 {
		t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
	}
	if got := academyUp.calls()[0].Body; got != `{"name_ar":"دبلومة","study_type":"diploma"}` {
		t.Fatalf("upstream body = %q", got)
	}
}

func TestCatalog_CreatedStatusesAreRelayed(t *testing.T) {
	authUp := newUpstream(t, 200, `{}`)
	academyUp := newUpstream(t, 201, `{"key":"diploma-x"}`)
	p := newSplitProxy(t, authUp, academyUp)
	w := do(p.LevelsCreate, http.MethodPost, "/api/levels/create", `{"study_type":"diploma","name_ar":"x"}`, withToken())
	if w.Code != 201 || decode(t, w)["key"] != "diploma-x" {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}
