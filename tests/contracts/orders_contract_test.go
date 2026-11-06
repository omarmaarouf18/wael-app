package contracts

import (
	"os/exec"
	"strings"
	"testing"
)

// TestContract_OrdersAPIShape verifies the orders contract end to end:
// POST /academy/orders wire shape, web cart validation, app request grouping,
// and the console bulk endpoint. The named tests live in academy-service.
func TestContract_OrdersAPIShape(t *testing.T) {
	names := []string{
		"TestOrders_ContractShape",
		"TestOrders_CheckoutValidation",
		"TestOrders_AppRequestsGroupIntoOneOpenOrder",
		"TestAdminBulkRequests_MixedResultsAuditAndPayment",
	}
	pattern := "^(" + strings.Join(names, "|") + ")$"
	cmd := exec.Command("go", "test", "-v", "-count=1", "-run", pattern,
		"github.com/omarmaarouf18/wael-app/academy-service/internal/handlers")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("orders contract verification failed: %v\nOutput:\n%s", err, string(out))
	}
	outStr := string(out)
	for _, name := range names {
		if !strings.Contains(outStr, "PASS: "+name) {
			t.Errorf("contract verification missing %s pass:\n%s", name, outStr)
		}
	}
}
