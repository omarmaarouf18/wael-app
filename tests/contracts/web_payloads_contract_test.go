package contracts

import (
	"os/exec"
	"strings"
	"testing"
)

// TestContract_WebPayloadsMatchBackendDTOs verifies that the web client's payloads
// for signup, verify-otp, resend, login, refresh, purchase-request and orders
// decode into the backend's request structs with DisallowUnknownFields.
func TestContract_WebPayloadsMatchBackendDTOs(t *testing.T) {
	// 1. Auth service web payloads contract
	cmdAuth := exec.Command("go", "test", "-v", "-count=1", "-run", "^TestContract_WebPayloads_Auth$",
		"github.com/omarmaarouf18/wael-app/auth-service/internal/handlers")
	outAuth, err := cmdAuth.CombinedOutput()
	if err != nil {
		t.Fatalf("auth web payloads contract test failed: %v\nOutput:\n%s", err, string(outAuth))
	}
	if !strings.Contains(string(outAuth), "PASS: TestContract_WebPayloads_Auth") {
		t.Fatalf("auth web payloads contract test did not pass:\n%s", string(outAuth))
	}

	// 2. Academy service web payloads contract
	cmdAcad := exec.Command("go", "test", "-v", "-count=1", "-run", "^TestContract_WebPayloads_Academy$",
		"github.com/omarmaarouf18/wael-app/academy-service/internal/handlers")
	outAcad, err := cmdAcad.CombinedOutput()
	if err != nil {
		t.Fatalf("academy web payloads contract test failed: %v\nOutput:\n%s", err, string(outAcad))
	}
	if !strings.Contains(string(outAcad), "PASS: TestContract_WebPayloads_Academy") {
		t.Fatalf("academy web payloads contract test did not pass:\n%s", string(outAcad))
	}
}
