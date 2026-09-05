package logging

import (
	"context"
	"market_maker/pkg/telemetry"
	"testing"
	"time"
)

func TestZapLogger_OTelBridge(t *testing.T) {
	// 1. Setup OTel
	tel, err := telemetry.Setup("test-logger")
	if err != nil {
		t.Fatalf("OTel setup failed: %v", err)
	}
	defer func() {
		_ = tel.Shutdown(context.Background())
	}()

	// 2. Create Zap Logger
	logger, err := NewZapLogger("DEBUG")
	if err != nil {
		t.Fatalf("Zap logger creation failed: %v", err)
	}

	// 3. Log something
	logger.Info("Test OTel bridging", "key", "value")

	// Wait a bit for OTel batching (if any)
	time.Sleep(500 * time.Millisecond)

	// Since we are using stdoutlog, we just verify it doesn't crash
	// and produces output. In a full test we might capture stdout.
	logger.Debug("Debug message", "status", "testing")

	_ = logger.Sync() // Some writers don't support sync (like stdout in some envs), ignore error
}
