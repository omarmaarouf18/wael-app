package proxy

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

const testPackageID = "pkg_0123456789abcdef"

// iapRoutes joins routes() (proxy_test.go) so the generic guarantees (401
// without a call, 405, exact upstream mapping, stripped client headers, 503s,
// relayed 4xx with Retry-After) hold for the iOS package routes too.
func iapRoutes() []route {
	return []route{
		{
			name: "iap.packages.list", handler: func(p *Proxy) http.HandlerFunc { return p.IAPPackagesList },
			method: http.MethodGet, target: "/api/iap/packages?evil=1",
			upstreamMethod: http.MethodGet, upstreamPath: "/internal/admin/iap/packages",
			upstreamStatus: 200, upstreamReply: `{"packages":[]}`,
		},
		{
			name: "iap.packages.create", handler: func(p *Proxy) http.HandlerFunc { return p.IAPPackagesCreate },
			method: http.MethodPost, target: "/api/iap/packages/create",
			body:           `{"product_id":"com.wael.app.pkg.year1","title_ar":"الفرقة الأولى","title_en":"Year 1","subject_ids":["s1"],"published":false,"position":1}`,
			upstreamMethod: http.MethodPost, upstreamPath: "/internal/admin/iap/packages",
			upstreamBody:   `{"position":1,"product_id":"com.wael.app.pkg.year1","published":false,"subject_ids":["s1"],"title_ar":"الفرقة الأولى","title_en":"Year 1"}`,
			upstreamStatus: 201, upstreamReply: `{"id":"` + testPackageID + `"}`,
			wantStatus: 201,
		},
		{
			name: "iap.packages.update", handler: func(p *Proxy) http.HandlerFunc { return p.IAPPackagesUpdate },
			method: http.MethodPost, target: "/api/iap/packages/update",
			body:           `{"id":"` + testPackageID + `","title_en":"Year 1","available_until":null,"published":true}`,
			upstreamMethod: http.MethodPatch, upstreamPath: "/internal/admin/iap/packages/" + testPackageID,
			upstreamBody:   `{"available_until":null,"published":true,"title_en":"Year 1"}`,
			upstreamStatus: 200, upstreamReply: `{"id":"` + testPackageID + `"}`,
		},
		{
			name: "iap.packages.delete", handler: func(p *Proxy) http.HandlerFunc { return p.IAPPackagesDelete },
			method: http.MethodPost, target: "/api/iap/packages/delete",
			body:           `{"id":"` + testPackageID + `"}`,
			upstreamMethod: http.MethodDelete, upstreamPath: "/internal/admin/iap/packages/" + testPackageID,
			upstreamStatus: 200, upstreamReply: `{"status":"ok"}`,
		},
		{
			name: "iap.purchases", handler: func(p *Proxy) http.HandlerFunc { return p.IAPPurchases },
			method: http.MethodGet, target: "/api/iap/purchases?package_id=" + testPackageID + "&user_id=" + testID + "&limit=50&evil=1",
			upstreamMethod: http.MethodGet, upstreamPath: "/internal/admin/iap/purchases",
			upstreamQuery:  "limit=50&package_id=" + testPackageID + "&user_id=" + testID,
			upstreamStatus: 200, upstreamReply: `{"purchases":[]}`,
		},
		{
			name: "iap.coverage", handler: func(p *Proxy) http.HandlerFunc { return p.IAPCoverage },
			method: http.MethodGet, target: "/api/iap/coverage?x=y",
			upstreamMethod: http.MethodGet, upstreamPath: "/internal/admin/iap/coverage",
			upstreamStatus: 200, upstreamReply: `{"uncovered_subject_ids":[]}`,
		},
	}
}

func TestIAP_RoutesReachAcademyNotAuth(t *testing.T) {
	for _, rt := range iapRoutes() {
		t.Run(rt.name, func(t *testing.T) {
			authUp := newUpstream(t, 200, `{"items":[]}`)
			academyUp := newUpstream(t, rt.upstreamStatus, rt.upstreamReply)
			p := newSplitProxy(t, authUp, academyUp)
			w := do(rt.handler(p), rt.method, rt.target, rt.body, withToken())
			if w.Code != rt.want() {
				t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
			}
			if n := len(authUp.calls()); n != 0 {
				t.Fatalf("auth upstream was called %d times", n)
			}
			calls := academyUp.calls()
			if len(calls) != 1 {
				t.Fatalf("academy upstream calls = %d, want 1", len(calls))
			}
			if !strings.HasPrefix(calls[0].Path, "/internal/admin/iap/") {
				t.Fatalf("route left /internal/admin/iap/: %s", calls[0].Path)
			}
		})
	}
}

func TestIAP_BadInputIs400(t *testing.T) {
	long := strings.Repeat("س", maxIAPTitleRunes+1)
	longDesc := strings.Repeat("س", maxIAPDescRunes+1)
	tooMany := make([]string, maxIAPSubjects+1)
	for i := range tooMany {
		tooMany[i] = "s" + strconv.Itoa(i)
	}
	tooManyJSON, _ := json.Marshal(tooMany)
	const ok = `"title_ar":"x","title_en":"y"`
	for name, body := range map[string]string{
		"missing product":    `{` + ok + `}`,
		"bad product":        `{"product_id":"com.other.pkg.x",` + ok + `}`,
		"upper product":      `{"product_id":"com.wael.app.pkg.Year1",` + ok + `}`,
		"long product":       `{"product_id":"com.wael.app.pkg.` + strings.Repeat("a", maxIAPProductID) + `",` + ok + `}`,
		"missing title_ar":   `{"product_id":"com.wael.app.pkg.y1","title_en":"y"}`,
		"missing title_en":   `{"product_id":"com.wael.app.pkg.y1","title_ar":"x"}`,
		"blank title":        `{"product_id":"com.wael.app.pkg.y1","title_ar":"  ","title_en":"y"}`,
		"title too long":     `{"product_id":"com.wael.app.pkg.y1","title_ar":"` + long + `","title_en":"y"}`,
		"desc too long":      `{"product_id":"com.wael.app.pkg.y1",` + ok + `,"description_en":"` + longDesc + `"}`,
		"bad subject id":     `{"product_id":"com.wael.app.pkg.y1",` + ok + `,"subject_ids":["a/b"]}`,
		"dup subject id":     `{"product_id":"com.wael.app.pkg.y1",` + ok + `,"subject_ids":["a","a"]}`,
		"too many subjects":  `{"product_id":"com.wael.app.pkg.y1",` + ok + `,"subject_ids":` + string(tooManyJSON) + `}`,
		"bad date":           `{"product_id":"com.wael.app.pkg.y1",` + ok + `,"available_until":"2027-01-20"}`,
		"number date":        `{"product_id":"com.wael.app.pkg.y1",` + ok + `,"available_until":5}`,
		"float position":     `{"product_id":"com.wael.app.pkg.y1",` + ok + `,"position":1.5}`,
		"id on create":       `{"id":"` + testPackageID + `","product_id":"com.wael.app.pkg.y1",` + ok + `}`,
		"unknown field":      `{"product_id":"com.wael.app.pkg.y1",` + ok + `,"price":10}`,
		"string published":   `{"product_id":"com.wael.app.pkg.y1",` + ok + `,"published":"yes"}`,
		"trailing data":      `{"product_id":"com.wael.app.pkg.y1",` + ok + `}{}`,
		"not an object":      `[]`,
		"subject ids string": `{"product_id":"com.wael.app.pkg.y1",` + ok + `,"subject_ids":"s1"}`,
	} {
		t.Run("create/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.IAPPackagesCreate }, http.MethodPost, "/api/iap/packages/create", body)
		})
	}
	for name, body := range map[string]string{
		"missing id":       `{"title_ar":"x"}`,
		"slash id":         `{"id":"a/b","title_ar":"x"}`,
		"product_id sent":  `{"id":"` + testPackageID + `","product_id":"com.wael.app.pkg.y1"}`,
		"blank title":      `{"id":"` + testPackageID + `","title_en":""}`,
		"title too long":   `{"id":"` + testPackageID + `","title_ar":"` + long + `"}`,
		"bad date":         `{"id":"` + testPackageID + `","available_until":"soon"}`,
		"dup subject id":   `{"id":"` + testPackageID + `","subject_ids":["a","a"]}`,
		"unknown field":    `{"id":"` + testPackageID + `","color":"red"}`,
		"string position":  `{"id":"` + testPackageID + `","position":"1"}`,
		"desc too long":    `{"id":"` + testPackageID + `","description_ar":"` + longDesc + `"}`,
		"empty object":     `{}`,
		"published number": `{"id":"` + testPackageID + `","published":1}`,
	} {
		t.Run("update/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.IAPPackagesUpdate }, http.MethodPost, "/api/iap/packages/update", body)
		})
	}
	for name, body := range map[string]string{
		"missing id":  `{}`,
		"slash id":    `{"id":"a/b"}`,
		"extra field": `{"id":"` + testPackageID + `","force":true}`,
	} {
		t.Run("delete/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.IAPPackagesDelete }, http.MethodPost, "/api/iap/packages/delete", body)
		})
	}
	for name, target := range map[string]string{
		"bad package id": "/api/iap/purchases?package_id=../x",
		"bad user id":    "/api/iap/purchases?user_id=not-a-uuid",
		"limit zero":     "/api/iap/purchases?limit=0",
		"limit 101":      "/api/iap/purchases?limit=101",
		"limit text":     "/api/iap/purchases?limit=x",
	} {
		t.Run("purchases/"+name, func(t *testing.T) {
			catalogBad(t, func(p *Proxy) http.HandlerFunc { return p.IAPPurchases }, http.MethodGet, target, "")
		})
	}
}

// Fields are forwarded only when present; the date is normalised to RFC3339
// UTC and titles are cleaned like the academy does.
func TestIAPPackagesCreate_ForwardsCleanedFieldsOnly(t *testing.T) {
	authUp := newUpstream(t, 200, `{}`)
	academyUp := newUpstream(t, 201, `{"id":"`+testPackageID+`"}`)
	p := newSplitProxy(t, authUp, academyUp)
	body := `{"product_id":"  com.wael.app.pkg.year1 ","title_ar":" الفرقة\nالأولى ","title_en":"Year 1","description_ar":"سطر\nتاني","description_en":"","subject_ids":[],"available_until":"2027-01-20T23:59:59+02:00"}`
	w := do(p.IAPPackagesCreate, http.MethodPost, "/api/iap/packages/create", body, withToken())
	if w.Code != 201 {
		t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
	}
	want := `{"available_until":"2027-01-20T21:59:59Z","description_ar":"سطر تاني","description_en":"","product_id":"com.wael.app.pkg.year1","subject_ids":[],"title_ar":"الفرقةالأولى","title_en":"Year 1"}`
	if got := academyUp.calls()[0].Body; got != want {
		t.Fatalf("upstream body = %q\nwant          %q", got, want)
	}
}

func TestIAPPackagesUpdate_AbsentDateIsNotSent(t *testing.T) {
	authUp := newUpstream(t, 200, `{}`)
	academyUp := newUpstream(t, 200, `{}`)
	p := newSplitProxy(t, authUp, academyUp)
	w := do(p.IAPPackagesUpdate, http.MethodPost, "/api/iap/packages/update", `{"id":"`+testPackageID+`","position":0}`, withToken())
	if w.Code != 200 {
		t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
	}
	if got := academyUp.calls()[0].Body; got != `{"position":0}` {
		t.Fatalf("upstream body = %q", got)
	}
}

// The 409 that blocks deleting a package with purchases reaches the page
// with its code, so the dialog can say why.
func TestIAPPackagesDelete_PackageHasPurchasesIsRelayed(t *testing.T) {
	authUp := newUpstream(t, 200, `{}`)
	academyUp := newUpstream(t, 409, `{"error":"cannot delete package with existing purchases","code":"package_has_purchases","request_id":"r1"}`)
	p := newSplitProxy(t, authUp, academyUp)
	w := do(p.IAPPackagesDelete, http.MethodPost, "/api/iap/packages/delete", `{"id":"`+testPackageID+`"}`, withToken())
	if w.Code != 409 || decode(t, w)["code"] != "package_has_purchases" {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}
