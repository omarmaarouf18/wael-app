package contracts

import (
	"os/exec"
	"strings"
	"testing"
)

// Academy admin API contracts (Phase 4.1-4.4): verify the academy admin
// handler tests pass by name, pinning the middleware matrix, the auth and
// audit shapes, and the level/subject/video endpoint behaviors.

const academyHandlersPkg = "github.com/omarmaarouf18/wael-app/academy-service/internal/handlers"

func runAcademyAdminTests(t *testing.T, run string) string {
	t.Helper()
	cmd := exec.Command("go", "test", "-v", "-count=1", "-run", run, academyHandlersPkg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("academy admin contract verification failed (-run %s): %v\nOutput:\n%s", run, err, string(out))
	}
	return string(out)
}

func requirePass(t *testing.T, out string, names ...string) {
	t.Helper()
	for _, name := range names {
		if !strings.Contains(out, "PASS: "+name) {
			t.Errorf("contract verification missing %s pass:\n%s", name, out)
		}
	}
}

// 8. Admin auth chain: two-token middleware, verify failure mapping, and the
// audit-log shape (same {items,total,page,limit} as auth, newest first).
func TestContract_AcademyAdminAuth(t *testing.T) {
	out := runAcademyAdminTests(t, "^(TestAdminAuth_MiddlewareMatrix|TestAdminAuth_BadAdminToken401|TestAdminAuth_LockedOut429|TestAdminAuth_AuthDown503|TestAdminAuth_ForwardsTokensAndClientIP|TestAdminAuth_NoCaching|TestAdminVerify_mTLSRealPath|TestAdminVerify_mTLSForeignClientCertRejected|TestAdminVerify_mTLSServerNameVerified|TestAdminAuditLog_PaginationAndShape|TestAdminAudit_WriteFailureFailsCall|TestAdminHandler_UnknownPaths404|TestRouteIsolation)$")
	requirePass(t, out,
		"TestAdminAuth_MiddlewareMatrix",
		"TestAdminAuth_BadAdminToken401",
		"TestAdminAuth_LockedOut429",
		"TestAdminAuth_AuthDown503",
		"TestAdminAuth_ForwardsTokensAndClientIP",
		"TestAdminAuth_NoCaching",
		"TestAdminVerify_mTLSRealPath",
		"TestAdminVerify_mTLSForeignClientCertRejected",
		"TestAdminVerify_mTLSServerNameVerified",
		"TestAdminAuditLog_PaginationAndShape",
		"TestAdminAudit_WriteFailureFailsCall",
		"TestAdminHandler_UnknownPaths404",
		"TestRouteIsolation",
	)
}

// 9. Diploma levels: create-only-diploma, patch, delete guards, admin list
// shape, and student visibility of published levels only.
func TestContract_AcademyAdminLevels(t *testing.T) {
	out := runAcademyAdminTests(t, "^(TestAdminLevels_AuthWiring|TestAdminLevels_CreateValidation|TestAdminLevels_Patch|TestAdminLevels_Delete|TestAdminLevels_ListAndStudentVisibility|TestAdminLevels_AuditPerMutation)$")
	requirePass(t, out,
		"TestAdminLevels_AuthWiring",
		"TestAdminLevels_CreateValidation",
		"TestAdminLevels_Patch",
		"TestAdminLevels_Delete",
		"TestAdminLevels_ListAndStudentVisibility",
		"TestAdminLevels_AuditPerMutation",
	)
}

// 10. Subjects: validation, filters, publish gate, student visibility with
// prices still hidden, and per-mutation audit rows.
func TestContract_AcademyAdminSubjects(t *testing.T) {
	out := runAcademyAdminTests(t, "^(TestAdminSubjects_CreateValidation|TestAdminSubjects_List|TestAdminSubjects_Patch|TestAdminSubjects_PublishFlow|TestAdminSubjects_StudentVisibilityAndPrice|TestAdminSubjects_AuditPerMutation)$")
	requirePass(t, out,
		"TestAdminSubjects_CreateValidation",
		"TestAdminSubjects_List",
		"TestAdminSubjects_Patch",
		"TestAdminSubjects_PublishFlow",
		"TestAdminSubjects_StudentVisibilityAndPrice",
		"TestAdminSubjects_AuditPerMutation",
	)
}

// 11. Videos: YouTube parser, CRUD, reorder, soft delete with the published
// last-video guard, and the student gating/leak rules.
func TestContract_AcademyAdminVideos(t *testing.T) {
	out := runAcademyAdminTests(t, "^(TestParseYouTubeID_Good|TestParseYouTubeID_Bad|TestAdminVideos_AuthWiring|TestAdminVideos_CreateValidation|TestAdminVideos_ListAndPatch|TestAdminVideos_Reorder|TestAdminVideos_Delete|TestAdminVideos_StudentGatingAndLeaks|TestAdminVideos_AuditPerMutation)$")
	requirePass(t, out,
		"TestParseYouTubeID_Good",
		"TestParseYouTubeID_Bad",
		"TestAdminVideos_AuthWiring",
		"TestAdminVideos_CreateValidation",
		"TestAdminVideos_ListAndPatch",
		"TestAdminVideos_Reorder",
		"TestAdminVideos_Delete",
		"TestAdminVideos_StudentGatingAndLeaks",
		"TestAdminVideos_AuditPerMutation",
	)
}
