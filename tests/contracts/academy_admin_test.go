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
	out := runAcademyAdminTests(t, "^(TestAdminAuth_MiddlewareMatrix|TestAdminAuth_BadAdminToken401|TestAdminAuth_LockedOut429|TestAdminAuth_AuthDown503|TestAdminAuth_ForwardsTokensAndClientIP|TestAdminAuth_NoCaching|TestAdminVerify_mTLSRealPath|TestAdminVerify_mTLSForeignClientCertRejected|TestAdminVerify_mTLSServerNameVerified|TestAdminAuditLog_PaginationAndShape|TestAdminAudit_WriteFailureBestEffort|TestAdminHandler_UnknownPaths404|TestRouteIsolation)$")
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
		"TestAdminAudit_WriteFailureBestEffort",
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

// 12. Owned unpublished visibility: owners keep list/detail/play after
// unpublish (or under unpublished levels) until entitlement expiry;
// non-owners are hidden everywhere.
func TestContract_AcademyStudentVisibility(t *testing.T) {
	out := runAcademyAdminTests(t, "^(TestStudentVisibility_OwnedUnpublished|TestStudentVisibility_EntitlementExpiry|TestStudentVisibility_UnpublishedLevel|TestStudentVisibility_OwnedDraftPagination|TestPlayVideoEndpoint)$")
	requirePass(t, out,
		"TestStudentVisibility_OwnedUnpublished",
		"TestStudentVisibility_EntitlementExpiry",
		"TestStudentVisibility_UnpublishedLevel",
		"TestStudentVisibility_OwnedDraftPagination",
		"TestPlayVideoEndpoint",
	)
}

// 13. Requests: list with pending_count, accept flow (entitlement + payment_record + audit + notify),
// reject flow, concurrency and retries.
func TestContract_AcademyAdminRequests(t *testing.T) {
	out := runAcademyAdminTests(t, "^(TestAdminRequests_List|TestAdminRequests_Accept_R4Matrix|TestAdminRequests_Accept_PreexistingManualGrant|TestAdminRequests_Accept_Concurrency|TestAdminRequests_Reject|TestAdminRequests_NotificationBestEffort)$")
	requirePass(t, out,
		"TestAdminRequests_List",
		"TestAdminRequests_Accept_R4Matrix",
		"TestAdminRequests_Accept_PreexistingManualGrant",
		"TestAdminRequests_Accept_Concurrency",
		"TestAdminRequests_Reject",
		"TestAdminRequests_NotificationBestEffort",
	)
}

// 14. Entitlements: list by user, grant with payment record, revoke with preserved payment record,
// and R1 revocation impact across list/detail/play/my entitlements.
func TestContract_AcademyAdminEntitlements(t *testing.T) {
	out := runAcademyAdminTests(t, "^(TestAdminEntitlements_List|TestAdminEntitlements_Grant|TestAdminEntitlements_Revoke|TestAdminEntitlements_RevocationImpactOnR1)$")
	requirePass(t, out,
		"TestAdminEntitlements_List",
		"TestAdminEntitlements_Grant",
		"TestAdminEntitlements_Revoke",
		"TestAdminEntitlements_RevocationImpactOnR1",
	)
}

// 15. Files (SPEC Phase 5.1/5.3): multipart PDF upload with the magic-byte and
// MAX_PDF_BYTES checks (413), server UUID storage keys, object cleanup when the
// row insert fails, list without storage keys, delete of row then object, and
// one audit entry per mutation; every other admin route keeps the 1 MiB cap.
func TestContract_AcademyAdminFiles(t *testing.T) {
	out := runAcademyAdminTests(t, "^(TestAdminFiles_AuthWiring|TestAdminFiles_UploadListDelete|TestAdminFiles_DeleteWhenObjectAlreadyGone|TestAdminFiles_ClientMetadataIgnored|TestAdminFiles_UploadValidation|TestAdminFiles_SizeCap|TestAdminFiles_FailureCleanup|TestIsAdminUploadRoute)$")
	requirePass(t, out,
		"TestAdminFiles_AuthWiring",
		"TestAdminFiles_UploadListDelete",
		"TestAdminFiles_DeleteWhenObjectAlreadyGone",
		"TestAdminFiles_ClientMetadataIgnored",
		"TestAdminFiles_UploadValidation",
		"TestAdminFiles_SizeCap",
		"TestAdminFiles_FailureCleanup",
		"TestIsAdminUploadRoute",
	)
}

// 16. Student download (SPEC Phase 5.2, R1/R3/R6/R8, D4): ownership checked on
// every call before the stored object is touched; owned 200 with the exact
// bytes and the R6 headers; unowned, revoked, expired 403; another subject's
// file, unknown ids and hidden subjects 404; Download tier 429 with
// Retry-After; storage failure 503 without detail; a 20 MiB file starts within
// the gateway's 2 s; sanitized Content-Disposition.
func TestContract_AcademyFileDownload(t *testing.T) {
	out := runAcademyAdminTests(t, "^(TestDownloadFile_OwnedStreamsExactBytes|TestDownloadFile_Refusals|TestDownloadFile_RateLimit429|TestDownloadFile_FailClosed503|TestDownloadFile_20MBWithinGatewayBudget|TestContentDisposition_Sanitized)$")
	requirePass(t, out,
		"TestDownloadFile_OwnedStreamsExactBytes",
		"TestDownloadFile_Refusals",
		"TestDownloadFile_RateLimit429",
		"TestDownloadFile_FailClosed503",
		"TestDownloadFile_20MBWithinGatewayBudget",
		"TestContentDisposition_Sanitized",
	)
}
