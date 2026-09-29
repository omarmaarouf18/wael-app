package handlerutil

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
)

func TestShipSecurityEventNonBlocking(t *testing.T) {
	// Setup invalid configuration to guarantee SDK calls fail
	CwLogGroup = "test-group"
	CwEnabled = true
	CwClient = cloudwatchlogs.NewFromConfig(aws.Config{})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	start := time.Now()
	// Call ShipSecurityEvent which runs in a goroutine
	ShipSecurityEvent(ctx, "TEST_EVENT", "test-service", "actor", "tenant", "detail", "127.0.0.1")
	duration := time.Since(start)

	// Verify that it runs asynchronously and does not block
	if duration > 100*time.Millisecond {
		t.Errorf("ShipSecurityEvent blocked for %v, expected instant return", duration)
	}
}

func TestShipSecurityEvent_AgnosticFailureModes(t *testing.T) {
	// Backup original globals
	origCwClient := CwClient
	origCwLogGroup := CwLogGroup
	origCwEnabled := CwEnabled
	defer func() {
		CwClient = origCwClient
		CwLogGroup = origCwLogGroup
		CwEnabled = origCwEnabled
	}()

	t.Run("DisabledCloudWatchDoesNotBlockOrPanic", func(t *testing.T) {
		CwEnabled = false
		CwClient = nil
		CwLogGroup = ""

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		start := time.Now()
		// Should return immediately, not block, and not panic
		ShipSecurityEvent(ctx, "TEST_EVENT", "test-service", "actor", "tenant", "detail", "127.0.0.1")
		duration := time.Since(start)

		if duration > 100*time.Millisecond {
			t.Errorf("ShipSecurityEvent blocked for %v when disabled", duration)
		}
	})

	t.Run("NilClientWithEnabledDoesNotBlockOrPanic", func(t *testing.T) {
		CwEnabled = true
		CwClient = nil
		CwLogGroup = "test-group"

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		start := time.Now()
		// Should return immediately, not block, and not panic
		ShipSecurityEvent(ctx, "TEST_EVENT", "test-service", "actor", "tenant", "detail", "127.0.0.1")
		duration := time.Since(start)

		if duration > 100*time.Millisecond {
			t.Errorf("ShipSecurityEvent blocked for %v with nil client", duration)
		}
	})

	t.Run("UnreachableLogSinkDoesNotBlockOrPanic", func(t *testing.T) {
		CwLogGroup = "test-group"
		CwEnabled = true
		// Invalid client configurations to guarantee failures and timeouts
		CwClient = cloudwatchlogs.NewFromConfig(aws.Config{})

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		start := time.Now()
		// Runs in a goroutine, should not block the caller even if log sink fails
		ShipSecurityEvent(ctx, "TEST_EVENT", "test-service", "actor", "tenant", "detail", "127.0.0.1")
		duration := time.Since(start)

		if duration > 100*time.Millisecond {
			t.Errorf("ShipSecurityEvent blocked for %v when log sink is unreachable", duration)
		}

		// Wait slightly to let the goroutine execute and ensure it doesn't panic
		time.Sleep(50 * time.Millisecond)
	})
}

func TestGetClientIP(t *testing.T) {
	// 1. Nil request -> returns ""
	if ip := GetClientIP(nil); ip != "" {
		t.Errorf("Expected empty string for nil request, got %q", ip)
	}
}
