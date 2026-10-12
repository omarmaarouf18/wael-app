package contracts

import (
	"os/exec"
	"strings"
	"testing"
)

// Broadcast contracts (SPEC Phase 7, ADR-0013): verify the broadcast unit
// tests pass by name, pinning validation, audiences, idempotent fan-out,
// retry, rate limits and audit. The row-shape test
// (TestBroadcast_RowMatchesAppContract) pins exactly the fields the app's
// notification_repository.dart parses (id, user_id, title, title_ar, body,
// body_ar, type, subject_id, read, created_at) and the Arabic-only base
// fill; the console error-code lists pin every admin code to ar/en text.

const notificationHandlersPkg = "github.com/omarmaarouf18/wael-app/notification-service/internal/handlers"
const notificationStorePkg = "github.com/omarmaarouf18/wael-app/notification-service/internal/store"
const academyStorePkg = "github.com/omarmaarouf18/wael-app/academy-service/internal/store"

func runNotificationTests(t *testing.T, pkg, run string) string {
	t.Helper()
	cmd := exec.Command("go", "test", "-v", "-count=1", "-run", run, pkg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("broadcast contract verification failed (-run %s): %v\nOutput:\n%s", run, err, string(out))
	}
	return string(out)
}

func requireBroadcastPass(t *testing.T, out string, names ...string) {
	t.Helper()
	for _, name := range names {
		if !strings.Contains(out, "PASS: "+name) {
			t.Errorf("contract verification missing %s pass:\n%s", name, out)
		}
	}
}

// Broadcast admin API: auth chain, validation, preview, fan-out, retry,
// rate limit and audit behavior.
func TestContract_BroadcastAdminAPI(t *testing.T) {
	out := runNotificationTests(t, notificationHandlersPkg, "^(TestBroadcast_AuthGuards|TestBroadcast_VerifyDownFailsClosed|TestBroadcast_Validation|TestBroadcast_Preview|TestBroadcast_CreateFanoutDone|TestBroadcast_ArabicOnlyFillsBase|TestBroadcast_EmptyAudience|TestBroadcast_SubjectAudienceAndUnknownSubject|TestBroadcast_PartialFailureThenRetry|TestBroadcast_RetryWhileSendingConflicts|TestBroadcast_RestartResumeDeliversOnce|TestBroadcast_RateLimit|TestBroadcast_AuditCarriesNoMessageText|TestBroadcast_HistoryAndGet|TestBroadcast_RowMatchesAppContract)$")
	requireBroadcastPass(t, out,
		"TestBroadcast_AuthGuards",
		"TestBroadcast_VerifyDownFailsClosed",
		"TestBroadcast_Validation",
		"TestBroadcast_Preview",
		"TestBroadcast_CreateFanoutDone",
		"TestBroadcast_ArabicOnlyFillsBase",
		"TestBroadcast_EmptyAudience",
		"TestBroadcast_SubjectAudienceAndUnknownSubject",
		"TestBroadcast_PartialFailureThenRetry",
		"TestBroadcast_RetryWhileSendingConflicts",
		"TestBroadcast_RestartResumeDeliversOnce",
		"TestBroadcast_RateLimit",
		"TestBroadcast_AuditCarriesNoMessageText",
		"TestBroadcast_HistoryAndGet",
		"TestBroadcast_RowMatchesAppContract",
	)
}

// Broadcast persistence: idempotent targets, counts, audit log.
func TestContract_BroadcastStore(t *testing.T) {
	out := runNotificationTests(t, notificationStorePkg, "^(TestMemoryStore_Broadcast)$")
	requireBroadcastPass(t, out,
		"TestMemoryStore_Broadcast",
	)
}

// Broadcast audience resolution: active owners only (SPEC R1), paging, and
// the admin route guards.
func TestContract_BroadcastAudience(t *testing.T) {
	out := runNotificationTests(t, academyHandlersPkg, "^(TestAdminBroadcastAudience_ActiveOnly|TestAdminBroadcastAudience_Paging|TestAdminBroadcastAudience_NotFoundAndGuards)$")
	requireBroadcastPass(t, out,
		"TestAdminBroadcastAudience_ActiveOnly",
		"TestAdminBroadcastAudience_Paging",
		"TestAdminBroadcastAudience_NotFoundAndGuards",
	)
	out = runNotificationTests(t, academyStorePkg, "^(TestMemoryStore_ActiveOwnerIDs)$")
	requireBroadcastPass(t, out,
		"TestMemoryStore_ActiveOwnerIDs",
	)
}
