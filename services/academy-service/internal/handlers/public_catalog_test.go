package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

// Content identifiers seeded into the public catalog fixture. None of them may
// appear in any public catalog response (ADR-0014 W2a).
const (
	pubVideoID    = "vid-pub-secret-1"
	pubYouTubeID  = "dQw4w9WgXcQ"
	pubFileID     = "file-pub-secret-1"
	pubStorageKey = "storage-key-secret-1"
	pubDraftID    = "subj-pub-draft"
	pubHiddenID   = "subj-pub-hidden-level"
	pubOpenID     = "subj-pub-open"
	pubHiddenLvl  = "diploma-pub-hidden"
	pubExpiredID  = "subj-pub-expired"
)

// publicCatalogFixture: one published subject (with a published video, an
// unpublished video and a file), one draft subject, and one published subject
// under an unpublished diploma. Prices are not exposed to app students
// (exposePrice=false), which must not affect the public catalog.
func publicCatalogFixture(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(false)
	ctx := context.Background()
	exp := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	if err := s.Store.CreateLevel(ctx, &models.Level{Key: pubHiddenLvl, StudyType: models.StudyTypeDiploma, TitleAr: "دبلوم", Published: false}); err != nil {
		t.Fatalf("CreateLevel: %v", err)
	}
	for _, subj := range []*models.Subject{
		{ID: pubOpenID, LevelKey: "bachelor-y1", Term: "first", TitleAr: "مادة منشورة", TitleEn: "Open subject", DescriptionAr: "وصف", DescriptionEn: "About", Price: 1800, Status: models.StatusPublished, Order: 1, AccessExpiresAt: exp},
		{ID: pubDraftID, LevelKey: "bachelor-y1", Term: "first", TitleAr: "مسودة", Price: 900, Status: models.StatusDraft, Order: 2, AccessExpiresAt: exp},
		{ID: pubHiddenID, LevelKey: pubHiddenLvl, Term: "first", TitleAr: "مخفية", Price: 700, Status: models.StatusPublished, Order: 3, AccessExpiresAt: exp},
		{ID: pubExpiredID, LevelKey: "bachelor-y1", Term: "first", TitleAr: "منتهية", Price: 500, Status: models.StatusPublished, Order: 4, AccessExpiresAt: time.Now().Add(-time.Hour)},
	} {
		if err := s.Store.CreateSubject(ctx, subj); err != nil {
			t.Fatalf("CreateSubject: %v", err)
		}
	}
	for _, v := range []*models.Video{
		{ID: pubVideoID, SubjectID: pubOpenID, Position: 1, TitleAr: "المحاضرة", YouTubeVideoID: pubYouTubeID, Published: true},
		{ID: pubVideoID + "-unpub", SubjectID: pubOpenID, Position: 2, TitleAr: "غير منشورة", YouTubeVideoID: "unpubYT0001", Published: false},
	} {
		if err := s.Store.CreateVideo(ctx, v); err != nil {
			t.Fatalf("CreateVideo: %v", err)
		}
	}
	if err := s.Store.CreateFile(ctx, &models.SubjectFile{ID: pubFileID, SubjectID: pubOpenID, Kind: "note", TitleAr: "مذكرة", SizeBytes: 1234, StorageKey: pubStorageKey}); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	return s
}

func doPublicCatalog(t *testing.T, s *Server, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(rec, req)
	return rec
}

// assertNoContentFields fails if a public body carries anything that opens
// content: video, YouTube or file ids, storage keys, download or play paths,
// or the content-list keys of the app's subject detail.
func assertNoContentFields(t *testing.T, body string) {
	t.Helper()
	for _, banned := range []string{
		pubVideoID, pubYouTubeID, "unpubYT0001", pubFileID, pubStorageKey,
		"youtube", "storage_key", "download", "/play", "playable",
		`"videos":[`, `"files":`, `"owned"`, `"request"`, "whatsapp",
		pubDraftID, pubHiddenID, pubHiddenLvl, pubExpiredID,
	} {
		if strings.Contains(body, banned) {
			t.Errorf("public body contains %q: %s", banned, body)
		}
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var publicSubjectKeys = []string{"access_expires_at", "counts", "currency", "description", "id", "instructor", "level_key", "price", "term", "title"}

func TestPublicCatalog_Shape(t *testing.T) {
	s := publicCatalogFixture(t)
	rec := doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog = %d (%s)", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=60" {
		t.Errorf("Cache-Control = %q, want public, max-age=60", cc)
	}
	if et := rec.Header().Get("ETag"); !strings.HasPrefix(et, `"`) || len(et) < 10 {
		t.Errorf("ETag = %q, want a quoted strong tag", et)
	}
	body := rec.Body.String()
	assertNoContentFields(t, body)

	var cat models.PublicCatalogDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &cat); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var gotTypes []string
	for _, st := range cat.StudyTypes {
		gotTypes = append(gotTypes, st.Key)
	}
	if strings.Join(gotTypes, ",") != "bachelor,diploma,vocational" {
		t.Fatalf("study types = %v", gotTypes)
	}
	var found []models.PublicSubjectDTO
	for _, st := range cat.StudyTypes {
		if st.Key == models.StudyTypeDiploma && len(st.Levels) != 0 {
			t.Errorf("unpublished diploma listed: %+v", st.Levels)
		}
		for _, l := range st.Levels {
			if l.Subjects == nil {
				t.Errorf("level %s subjects is null, want []", l.Key)
			}
			found = append(found, l.Subjects...)
		}
	}
	if len(found) != 1 || found[0].ID != pubOpenID {
		t.Fatalf("subjects = %+v, want only %s", found, pubOpenID)
	}
	got := found[0]
	if got.Price != 1800 || got.Currency != "EGP" {
		t.Errorf("price = %d %q, want 1800 EGP even with show_prices off", got.Price, got.Currency)
	}
	if got.Instructor != models.AcademyInstructor {
		t.Errorf("instructor = %+v", got.Instructor)
	}
	if got.Counts != (models.SubjectCountsDTO{Videos: 1, Books: 0, Notes: 1}) {
		t.Errorf("counts = %+v, want 1 published video and 1 note", got.Counts)
	}
	if got.Title.En != "Open subject" || got.Description.Ar != "وصف" || got.LevelKey != "bachelor-y1" {
		t.Errorf("subject = %+v", got)
	}

	var raw struct {
		StudyTypes []struct {
			Levels []struct {
				Subjects []map[string]any `json:"subjects"`
			} `json:"levels"`
		} `json:"study_types"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	for _, st := range raw.StudyTypes {
		for _, l := range st.Levels {
			for _, subj := range l.Subjects {
				if k := strings.Join(sortedKeys(subj), ","); k != strings.Join(publicSubjectKeys, ",") {
					t.Errorf("subject keys = %s, want %s", k, strings.Join(publicSubjectKeys, ","))
				}
			}
		}
	}
}

func TestPublicSubject_Shape(t *testing.T) {
	s := publicCatalogFixture(t)
	rec := doPublicCatalog(t, s, http.MethodGet, "/academy/public/subjects/"+pubOpenID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("subject = %d (%s)", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=60" {
		t.Errorf("Cache-Control = %q", cc)
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("missing ETag")
	}
	body := rec.Body.String()
	assertNoContentFields(t, body)

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := append(append([]string{}, publicSubjectKeys...), "level")
	sort.Strings(want)
	if k := strings.Join(sortedKeys(raw), ","); k != strings.Join(want, ",") {
		t.Fatalf("keys = %s, want %s", k, strings.Join(want, ","))
	}
	var dto models.PublicSubjectDetailDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode dto: %v", err)
	}
	if dto.Price != 1800 || dto.Level.Key != "bachelor-y1" || dto.Level.StudyType != models.StudyTypeBachelor {
		t.Errorf("dto = %+v", dto)
	}
}

func TestPublicSubject_NotFound(t *testing.T) {
	s := publicCatalogFixture(t)
	for name, id := range map[string]string{
		"draft_subject":           pubDraftID,
		"unknown_id":              "no-such-subject",
		"subject_of_hidden_level": pubHiddenID,
		"expired_subject":         pubExpiredID,
		"overlong_id":             strings.Repeat("a", 65),
	} {
		t.Run(name, func(t *testing.T) {
			rec := doPublicCatalog(t, s, http.MethodGet, "/academy/public/subjects/"+id, nil)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("404 Cache-Control = %q, want no-store", cc)
			}
			var e map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if e["code"] != "not_found" {
				t.Errorf("code = %v", e["code"])
			}
			assertNoContentFields(t, rec.Body.String())
		})
	}
}

// A subject published in the console appears with no extra step; a subject
// moved back to draft disappears.
func TestPublicCatalog_FollowsPublishState(t *testing.T) {
	s := publicCatalogFixture(t)
	ctx := context.Background()
	subj, err := s.Store.GetSubjectByID(ctx, pubDraftID)
	if err != nil || subj == nil {
		t.Fatalf("GetSubjectByID: %v", err)
	}
	subj.Status = models.StatusPublished
	if err := s.Store.UpdateSubject(ctx, subj); err != nil {
		t.Fatalf("UpdateSubject: %v", err)
	}
	if rec := doPublicCatalog(t, s, http.MethodGet, "/academy/public/subjects/"+pubDraftID, nil); rec.Code != http.StatusOK {
		t.Fatalf("published subject = %d, want 200", rec.Code)
	}
	if rec := doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", nil); !strings.Contains(rec.Body.String(), pubDraftID) {
		t.Fatalf("published subject missing from catalog: %s", rec.Body.String())
	}
	subj.Status = models.StatusDraft
	if err := s.Store.UpdateSubject(ctx, subj); err != nil {
		t.Fatalf("UpdateSubject: %v", err)
	}
	if rec := doPublicCatalog(t, s, http.MethodGet, "/academy/public/subjects/"+pubDraftID, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("draft again = %d, want 404", rec.Code)
	}
}

func TestPublicCatalog_ETagNotModified(t *testing.T) {
	s := publicCatalogFixture(t)
	for _, path := range []string{"/academy/public/catalog", "/academy/public/subjects/" + pubOpenID} {
		first := doPublicCatalog(t, s, http.MethodGet, path, nil)
		etag := first.Header().Get("ETag")
		if first.Code != http.StatusOK || etag == "" {
			t.Fatalf("%s first = %d etag %q", path, first.Code, etag)
		}
		again := doPublicCatalog(t, s, http.MethodGet, path, nil)
		if again.Header().Get("ETag") != etag {
			t.Fatalf("%s ETag not stable: %q vs %q", path, etag, again.Header().Get("ETag"))
		}
		for _, inm := range []string{etag, "W/" + etag, `"other", ` + etag} {
			rec := doPublicCatalog(t, s, http.MethodGet, path, map[string]string{"If-None-Match": inm})
			if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
				t.Fatalf("%s If-None-Match %q = %d body %d, want 304 empty", path, inm, rec.Code, rec.Body.Len())
			}
			if rec.Header().Get("ETag") != etag || rec.Header().Get("Cache-Control") != "public, max-age=60" {
				t.Errorf("304 headers = %v", rec.Header())
			}
		}
		if rec := doPublicCatalog(t, s, http.MethodGet, path, map[string]string{"If-None-Match": `"stale"`}); rec.Code != http.StatusOK {
			t.Fatalf("%s stale If-None-Match = %d, want 200", path, rec.Code)
		}
	}
	// The tag changes when the data changes.
	before := doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", nil).Header().Get("ETag")
	subj, _ := s.Store.GetSubjectByID(context.Background(), pubOpenID)
	subj.Price = 2000
	if err := s.Store.UpdateSubject(context.Background(), subj); err != nil {
		t.Fatalf("UpdateSubject: %v", err)
	}
	if after := doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", nil).Header().Get("ETag"); after == before {
		t.Fatal("ETag unchanged after a price change")
	}
}

// No JWT is needed, a JWT changes nothing (no per-user data), and the
// gateway secret is still required.
func TestPublicCatalog_Auth(t *testing.T) {
	s := publicCatalogFixture(t)
	anon := doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", nil)
	if err := s.Store.Grant(context.Background(), &models.Entitlement{UserID: "user-pub-1", SubjectID: pubOpenID}); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	withTok := doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", map[string]string{"Authorization": "Bearer " + makeStudentToken(t, "user-pub-1")})
	if anon.Code != http.StatusOK || withTok.Code != http.StatusOK || anon.Body.String() != withTok.Body.String() {
		t.Fatalf("anon %d / token %d differ:\n%s\n%s", anon.Code, withTok.Code, anon.Body.String(), withTok.Body.String())
	}

	for _, path := range []string{"/academy/public/catalog", "/academy/public/subjects/" + pubOpenID} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.PublicHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without gateway secret = %d, want 401", path, rec.Code)
		}
	}
}

// The StudentAuth bypass is exact: nothing else under /academy/ borrows it.
func TestPublicCatalog_BypassIsExact(t *testing.T) {
	s := publicCatalogFixture(t)
	for _, path := range []string{
		"/academy/public/subjects/" + pubOpenID + "/files/" + pubFileID + "/download",
		"/academy/public/subjects/x/../../../me/entitlements",
		"/academy/public/catalog/extra",
		"/academy/public/",
		"/academy/public/subjects/",
		"/academy/me/entitlements",
	} {
		rec := doPublicCatalog(t, s, http.MethodGet, path, nil)
		if rec.Code == http.StatusOK {
			t.Errorf("%s without JWT = 200, want refused: %s", path, rec.Body.String())
		}
		assertNoContentFields(t, rec.Body.String())
	}
	for _, p := range []string{"/academy/public/catalog", "/academy/public/subjects/abc"} {
		if !isPublicCatalogPath(p) {
			t.Errorf("isPublicCatalogPath(%q) = false", p)
		}
	}
	for _, p := range []string{"/academy/public/subjects/", "/academy/public/subjects/a/b", "/academy/public/catalog/", "/academy/public", "/academy/subjects/abc"} {
		if isPublicCatalogPath(p) {
			t.Errorf("isPublicCatalogPath(%q) = true", p)
		}
	}
}

func TestPublicCatalog_MethodNotAllowed(t *testing.T) {
	s := publicCatalogFixture(t)
	for _, path := range []string{"/academy/public/catalog", "/academy/public/subjects/" + pubOpenID} {
		rec := doPublicCatalog(t, s, http.MethodPost, path, nil)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s = %d, want 405", path, rec.Code)
		}
	}
}

// Per-IP read tier: over the limit is 429 with Retry-After; another IP is
// not affected.
func TestPublicCatalog_RateLimitPerIP(t *testing.T) {
	s := publicCatalogFixture(t)
	_, lim := setupTestLimiter(t, 2, 10, 10, 10)
	s.Limiter = lim
	ip := map[string]string{"X-Forwarded-For": "203.0.113.7"}
	for i := 0; i < 2; i++ {
		if rec := doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", ip); rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i+1, rec.Code)
		}
	}
	rec := doPublicCatalog(t, s, http.MethodGet, "/academy/public/subjects/"+pubOpenID, ip)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("over limit = %d Retry-After %q, want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
	other := doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", map[string]string{"X-Forwarded-For": "198.51.100.9"})
	if other.Code != http.StatusOK {
		t.Fatalf("other IP = %d, want 200", other.Code)
	}
}

// Outside local/test a missing limiter fails closed.
func TestPublicCatalog_NoLimiterFailsClosedInProduction(t *testing.T) {
	s := publicCatalogFixture(t)
	s.AppEnv = "production"
	s.Limiter = nil
	if rec := doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// Owner decision 2026-11-04: a published subject whose access_expires_at has
// passed is hidden from the public catalog and is 404 on the public subject
// route, from the moment it expires; the app still lists it (unchanged).
func TestPublicCatalog_HidesExpiredSubjects(t *testing.T) {
	s := publicCatalogFixture(t)
	ctx := context.Background()

	subj, err := s.Store.GetSubjectByID(ctx, pubOpenID)
	if err != nil || subj == nil {
		t.Fatalf("GetSubjectByID: %v", err)
	}
	if !strings.Contains(doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", nil).Body.String(), pubOpenID) {
		t.Fatal("unexpired subject missing from catalog")
	}
	subj.AccessExpiresAt = time.Now().Add(-time.Second)
	if err := s.Store.UpdateSubject(ctx, subj); err != nil {
		t.Fatalf("UpdateSubject: %v", err)
	}
	cat := doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", nil)
	if cat.Code != http.StatusOK || strings.Contains(cat.Body.String(), pubOpenID) {
		t.Fatalf("expired subject still in catalog (%d): %s", cat.Code, cat.Body.String())
	}
	if rec := doPublicCatalog(t, s, http.MethodGet, "/academy/public/subjects/"+pubOpenID, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("expired subject = %d, want 404", rec.Code)
	}

	// The app's list for a student still shows the expired subject.
	app := studentGet(t, s, makeStudentToken(t, "user-pub-app"), "/academy/subjects?level=bachelor-y1")
	if app.Code != http.StatusOK || !strings.Contains(app.Body.String(), pubOpenID) || !strings.Contains(app.Body.String(), pubExpiredID) {
		t.Fatalf("app list changed (%d): %s", app.Code, app.Body.String())
	}

	// Renewing the window brings it back.
	subj.AccessExpiresAt = time.Now().Add(time.Hour)
	if err := s.Store.UpdateSubject(ctx, subj); err != nil {
		t.Fatalf("UpdateSubject: %v", err)
	}
	if rec := doPublicCatalog(t, s, http.MethodGet, "/academy/public/subjects/"+pubOpenID, nil); rec.Code != http.StatusOK {
		t.Fatalf("renewed subject = %d, want 200", rec.Code)
	}
}

func TestPublicSubjectExpired(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for name, c := range map[string]struct {
		exp  time.Time
		want bool
	}{
		"zero_never_expires": {time.Time{}, false},
		"future":             {now.Add(time.Nanosecond), false},
		"exactly_now":        {now, true},
		"past":               {now.Add(-time.Hour), true},
	} {
		if got := publicSubjectExpired(&models.Subject{AccessExpiresAt: c.exp}, now); got != c.want {
			t.Errorf("%s: expired = %v, want %v", name, got, c.want)
		}
	}
}

func TestPublicCatalog_TeacherProfile(t *testing.T) {
	s := publicCatalogFixture(t)

	// 1. Default teacher profile when settings are empty
	rec := doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog GET = %d (%s)", rec.Code, rec.Body.String())
	}
	var cat models.PublicCatalogDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &cat); err != nil {
		t.Fatal(err)
	}
	if cat.TeacherProfile.NameAr != models.AcademyInstructor.Ar || cat.TeacherProfile.NameEn != models.AcademyInstructor.En {
		t.Errorf("default teacher profile name mismatch: %+v", cat.TeacherProfile)
	}
	if cat.TeacherProfile.BioAr != "" || cat.TeacherProfile.TitlesAr != "" {
		t.Errorf("default teacher profile should have empty titles and bio: %+v", cat.TeacherProfile)
	}

	// 2. Custom teacher profile configured
	ctx := context.Background()
	_ = s.Store.SaveAppSettings(ctx, &models.AppSettings{
		ShowPrices:      true,
		SupportWhatsApp: "201012345678",
		TeacherProfile: models.TeacherProfile{
			NameAr:   "أ.د. وائل السعيد",
			NameEn:   "Prof. Wael El Saeed",
			TitlesAr: "محاضر القانون المدني",
			TitlesEn: "Civil Law Lecturer",
			BioAr:    "خبرة أكثر من 15 عاماً في تدريس مواد الحقوق.",
			BioEn:    "Over 15 years experience teaching law.",
		},
	})

	rec2 := doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("catalog GET after settings = %d (%s)", rec2.Code, rec2.Body.String())
	}
	var cat2 models.PublicCatalogDTO
	if err := json.Unmarshal(rec2.Body.Bytes(), &cat2); err != nil {
		t.Fatal(err)
	}
	if cat2.TeacherProfile.NameAr != "أ.د. وائل السعيد" || cat2.TeacherProfile.BioEn != "Over 15 years experience teaching law." {
		t.Errorf("custom teacher profile mismatch: %+v", cat2.TeacherProfile)
	}
}

func TestPublicCatalog_NoSettingsLeak(t *testing.T) {
	s := publicCatalogFixture(t)
	ctx := context.Background()

	// Populate sensitive settings
	_ = s.Store.SaveAppSettings(ctx, &models.AppSettings{
		ShowPrices:      false,
		SupportWhatsApp: "201012345678",
		CenterNameAr:    "مقر الأكاديمية بالمنصورة",
		CenterNameEn:    "Mansoura Academy",
		CenterAddressAr: "شارع 18",
		CenterAddressEn: "18th Street",
		CenterHoursAr:   "9 صباحا - 5 مساء",
		CenterHoursEn:   "9 AM - 5 PM",
		CenterMapURL:    "https://maps.example.com",
		PaymentMethods: []models.PaymentMethod{
			{Kind: "wallet", Label: "Secret Wallet", NumberOrAddress: "01099999999"},
		},
		TeacherProfile: models.TeacherProfile{
			NameAr: "وائل السعيد",
			BioAr:  "سيرة المحاضر",
		},
	})

	rec := doPublicCatalog(t, s, http.MethodGet, "/academy/public/catalog", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog GET = %d (%s)", rec.Code, rec.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}

	// Verify top-level keys
	for k := range raw {
		if k != "study_types" && k != "teacher_profile" {
			t.Errorf("unexpected top-level key in public catalog: %q", k)
		}
	}

	// Verify no internal settings leak anywhere into the serialized JSON
	bodyStr := rec.Body.String()
	leakedStrings := []string{
		"show_prices", "support_whatsapp", "payment_methods", "Secret Wallet", "01099999999",
		"center_name_ar", "center_address_ar", "center_hours_ar", "center_map_url",
	}
	for _, leak := range leakedStrings {
		if strings.Contains(bodyStr, leak) {
			t.Errorf("public catalog leaks internal settings property %q: %s", leak, bodyStr)
		}
	}
}
