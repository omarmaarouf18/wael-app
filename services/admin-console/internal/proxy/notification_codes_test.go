package proxy

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// notificationErrorCodes is the checked-in list of error codes the
// notification-service broadcast admin handlers can return to the console.
// The console relays upstream 4xx bodies with their code, so every code here
// needs an ar and en message in web/js/i18n.js (checked by
// web/test/errors.test.mjs against the academy list plus this one).
var notificationErrorCodes = []string{
	"unauthorized",
	"locked_out",
	"service_unavailable",
	"invalid_json",
	"method_not_allowed",
	"not_found",
	"invalid_audience",
	"invalid_title",
	"invalid_body",
	"broadcast_forbidden_word",
	"empty_audience",
	"subject_not_found",
	"broadcast_not_found",
	"broadcast_in_progress",
	"rate_limited",
}

// writeJSONCodes are returned via handlerutil.WriteJSON (429 paths that also
// set Retry-After), so the WriteSafeError grep below cannot see them.
var notificationWriteJSONCodes = []string{
	"locked_out",
	"rate_limited",
}

// TestNotificationErrorCodes_MatchHandlers fails when a broadcast admin
// handler returns a code that is not in notificationErrorCodes (the console
// would relay a code the page has no message for) or when the list names a
// code no handler returns anymore.
func TestNotificationErrorCodes_MatchHandlers(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "notification-service", "internal", "handlers")
	raw, err := os.ReadFile(filepath.Join(dir, "admin_broadcast.go"))
	if err != nil {
		t.Fatalf("read admin_broadcast.go: %v", err)
	}
	found := map[string]bool{}
	for _, code := range notificationWriteJSONCodes {
		found[code] = true
	}
	for _, line := range regexp.MustCompile(`\n`).Split(string(raw), -1) {
		if m := codeArg.FindStringSubmatch(line); m != nil {
			code := m[1]
			if sub := quoted.FindStringSubmatch(code); sub != nil {
				code = sub[1]
			} else if v, ok := errCodeValues[code]; ok {
				code = v
			} else {
				t.Fatalf("admin_broadcast.go: unknown code constant %s", code)
			}
			found[code] = true
		}
		if m := buildReturn.FindStringSubmatch(line); m != nil {
			found[m[1][1:len(m[1])-1]] = true
		}
	}
	var got []string
	for code := range found {
		got = append(got, code)
	}
	sort.Strings(got)
	want := append([]string(nil), notificationErrorCodes...)
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
