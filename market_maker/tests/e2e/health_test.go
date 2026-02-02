package e2e

import (
	"context"
	"fmt"
	"market_maker/internal/infrastructure/health"
	"market_maker/internal/mock"
	"market_maker/internal/risk"
	"market_maker/internal/trading/monitor"
	"market_maker/internal/trading/order"
	"market_maker/pkg/logging"
	"net/http"
	"testing"
	"time"

	"market_maker/internal/infrastructure/server"
)

// TestE2E_HealthSystem verifies the complete health check system
func TestE2E_HealthSystem(t *testing.T) {
	logger, _ := logging.NewZapLogger("INFO")

	// 1. Create mock components
	mockExchange := mock.NewMockExchange("test")
	orderExecutor := order.NewOrderExecutor(mockExchange, logger)
	riskMonitor := risk.NewRiskMonitor(
		mockExchange,
		logger,
		[]string{"BTCUSDT"},
		"1m",
		3.0,
		10,
		5,
		"All",
		nil,
	)

	// 2. Create health manager and register components
	healthManager := health.NewHealthManager(logger)
	healthManager.Register("exchange", func() error {
		return mockExchange.CheckHealth(context.Background())
	})
	healthManager.Register("order_executor", orderExecutor.CheckHealth)
	healthManager.Register("risk_monitor", riskMonitor.CheckHealth)

	// 3. Verify all components are healthy
	if !healthManager.IsHealthy() {
		t.Fatal("Health manager should report all components healthy")
	}

	status := healthManager.GetStatus()
	expectedComponents := []string{"exchange", "order_executor", "risk_monitor"}
	for _, comp := range expectedComponents {
		if status[comp] != "Healthy" {
			t.Errorf("Component %s should be healthy, got: %s", comp, status[comp])
		}
	}

	t.Log("✓ All components healthy")

	// 4. Test component failure detection
	healthManager.Register("failing_component", func() error {
		return fmt.Errorf("simulated failure")
	})

	if healthManager.IsHealthy() {
		t.Error("Health manager should detect failing component")
	}

	status = healthManager.GetStatus()
	if status["failing_component"] != "Unhealthy: simulated failure" {
		t.Errorf("Expected unhealthy status, got: %s", status["failing_component"])
	}

	t.Log("✓ Component failure detection works")
}

// TestE2E_HealthServer verifies the HTTP health endpoints
func TestE2E_HealthServer(t *testing.T) {
	logger, _ := logging.NewZapLogger("INFO")

	// 1. Create health manager with components
	healthManager := health.NewHealthManager(logger)
	mockExchange := mock.NewMockExchange("test")
	healthManager.Register("exchange", func() error {
		return mockExchange.CheckHealth(context.Background())
	})

	// 2. Start health server
	healthServer := server.NewHealthServer("9999", logger, healthManager)
	healthServer.UpdateStatus("test_key", "test_value")
	healthServer.Start()
	defer healthServer.Stop(context.Background())

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// 3. Test /health endpoint (liveness probe)
	resp, err := http.Get("http://localhost:9999/health")
	if err != nil {
		t.Fatalf("Failed to query /health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	t.Log("✓ /health endpoint returns 200 OK")

	// 4. Test /status endpoint (detailed diagnostics)
	resp, err = http.Get("http://localhost:9999/status")
	if err != nil {
		t.Fatalf("Failed to query /status endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Content-Type") != "application/json" {
		t.Error("Expected Content-Type: application/json")
	}

	t.Log("✓ /status endpoint returns JSON diagnostics")

	// 5. Test unhealthy state
	healthManager.Register("failing", func() error {
		return fmt.Errorf("fail")
	})

	resp, err = http.Get("http://localhost:9999/health")
	if err != nil {
		t.Fatalf("Failed to query /health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", resp.StatusCode)
	}

	t.Log("✓ /health endpoint returns 503 when unhealthy")
}

// TestE2E_ComponentHealthChecks verifies individual component health implementations
func TestE2E_ComponentHealthChecks(t *testing.T) {
	logger, _ := logging.NewZapLogger("INFO")

	t.Run("PriceMonitor_NotStarted", func(t *testing.T) {
		mockExchange := mock.NewMockExchange("test")
		pm := monitor.NewPriceMonitor(mockExchange, "BTCUSDT", logger)

		// Should be unhealthy when not started
		if err := pm.CheckHealth(); err == nil {
			t.Error("PriceMonitor should be unhealthy when not started")
		}
	})

	t.Run("OrderExecutor", func(t *testing.T) {
		mockExchange := mock.NewMockExchange("test")
		oe := order.NewOrderExecutor(mockExchange, logger)

		if err := oe.CheckHealth(); err != nil {
			t.Errorf("OrderExecutor should be healthy: %v", err)
		}
	})

	t.Run("RiskMonitor", func(t *testing.T) {
		mockExchange := mock.NewMockExchange("test")
		rm := risk.NewRiskMonitor(
			mockExchange,
			logger,
			[]string{"BTCUSDT"},
			"1m",
			3.0,
			10,
			5,
			"All",
			nil,
		)

		if err := rm.CheckHealth(); err != nil {
			t.Errorf("RiskMonitor should be healthy: %v", err)
		}
	})

	t.Run("MockExchange", func(t *testing.T) {
		mockExchange := mock.NewMockExchange("test")

		if err := mockExchange.CheckHealth(context.Background()); err != nil {
			t.Errorf("MockExchange should be healthy: %v", err)
		}
	})

	t.Log("✓ All component health checks pass")
}
