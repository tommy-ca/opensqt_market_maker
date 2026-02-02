package health

import (
	"fmt"
	"testing"
)

func TestHealthManager_Aggregation(t *testing.T) {
	hm := NewHealthManager(nil)

	// Initial state: Healthy (no checks)
	if !hm.IsHealthy() {
		t.Error("Empty health manager should be healthy")
	}

	// Add healthy check
	hm.Register("comp1", func() error { return nil })
	if !hm.IsHealthy() {
		t.Error("Healthy component should not fail manager")
	}

	// Add unhealthy check
	hm.Register("comp2", func() error { return fmt.Errorf("failed") })
	if hm.IsHealthy() {
		t.Error("Unhealthy component should fail manager")
	}

	status := hm.GetStatus()
	if status["comp1"] != "Healthy" {
		t.Errorf("Expected Healthy, got %s", status["comp1"])
	}
	if status["comp2"] != "Unhealthy: failed" {
		t.Errorf("Expected Unhealthy, got %s", status["comp2"])
	}
}
