package proxy

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// academyErrorCodes is the checked-in list of error codes the academy admin
// handlers (Phase 4.1-4.4) can return to the console. The console relays
// upstream 4xx bodies with their code, so every code here needs an ar and en
// message in web/js/i18n.js (checked by web/test/errors.test.mjs against the
// same list). Extracted with:
//
//	grep -rhoE 'WriteSafeError\(w, r, http\.Status[A-Za-z]+, ("[a-z_0-9]+"|handlerutil\.ErrCode[A-Za-z]+)|return nil, ("[a-z_0-9]+"|http\.Status[A-Za-z]+, "[a-z_0-9]+")' \
//	  services/academy-service/internal/handlers/admin_*.go
var academyErrorCodes = []string{
	"unauthorized",
	"locked_out",
	"service_unavailable",
	"invalid_json",
	"method_not_allowed",
	"not_found",
	"invalid_level_id",
	"invalid_study_type",
	"invalid_name",
	"level_not_found",
	"level_not_deletable",
	"level_has_subjects",
	"invalid_subject_id",
	"subject_not_found",
	"invalid_title",
	"invalid_description",
	"invalid_term",
	"invalid_price",
	"invalid_expires_at",
	"invalid_published",
	"subject_has_no_videos",
	"invalid_video_id",
	"video_not_found",
	"invalid_youtube_id",
	"invalid_duration_seconds",
	"invalid_video_order",
	"last_video_of_published_subject",
}

var (
	// codeArg matches the code argument of WriteSafeError, either a literal
	// or a handlerutil constant.
	codeArg = regexp.MustCompile(`WriteSafeError\(w, r, http\.Status[A-Za-z]+, ("[a-z_0-9]+"|handlerutil\.ErrCode[A-Za-z]+)`)
	// buildReturn matches the (nil, "code", ...) returns of the shared
	// subject builder and the lockout return of verifyAdminToken.
	buildReturn = regexp.MustCompile(`return nil, (?:http\.Status[A-Za-z]+, )?("[a-z_0-9]+")`)
	quoted      = regexp.MustCompile(`^"([a-z_0-9]+)"$`)

	errCodeValues = map[string]string{
		"handlerutil.ErrCodeUnauthorized": "unauthorized",
		"handlerutil.ErrCodeUnavailable":  "service_unavailable",
		"handlerutil.ErrCodeInvalidJSON":  "invalid_json",
		"handlerutil.ErrCodeConflict":     "conflict",
	}
)

// TestAcademyErrorCodes_MatchHandlers fails when an academy admin handler
// returns a code that is not in academyErrorCodes (the console would relay a
// code the page has no message for) or when the list names a code no handler
// returns anymore.
func TestAcademyErrorCodes_MatchHandlers(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "academy-service", "internal", "handlers")
	files := []string{"admin.go", "admin_levels.go", "admin_subjects.go", "admin_videos.go", "admin_youtube.go", "admin_validate.go"}
	found := map[string]bool{}
	for _, name := range files {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, line := range regexp.MustCompile(`\n`).Split(string(raw), -1) {
			if m := codeArg.FindStringSubmatch(line); m != nil {
				code := m[1]
				if sub := quoted.FindStringSubmatch(code); sub != nil {
					code = sub[1]
				} else if v, ok := errCodeValues[code]; ok {
					code = v
				} else {
					t.Fatalf("%s: unknown code constant %s", name, code)
				}
				found[code] = true
			}
			if m := buildReturn.FindStringSubmatch(line); m != nil {
				found[m[1][1:len(m[1])-1]] = true
			}
		}
	}
	var got []string
	for code := range found {
		got = append(got, code)
	}
	sort.Strings(got)
	want := append([]string(nil), academyErrorCodes...)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("handler codes = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("handler codes = %v, want %v", got, want)
		}
	}
}
