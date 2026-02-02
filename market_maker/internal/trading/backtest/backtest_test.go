package backtest

import (
	"context"
	simple "market_maker/internal/engine/simple"
	"market_maker/internal/pb"
	"market_maker/internal/trading/order"
	"market_maker/internal/trading/position"
	"market_maker/internal/trading/strategy"
	"market_maker/pkg/logging"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestBacktest_BasicFlow(t *testing.T) {
	setupTelemetry()

	// 1. Setup simulated components
	exch := NewSimulatedExchange()
	logger, _ := logging.NewZapLogger("DEBUG")

	orderExecutor := order.NewOrderExecutor(exch, logger)
	orderExecutor.SetRateLimit(1000000, 1000000) // Unlimited for backtest

	// We'll use a nil risk monitor for simplicity in this test
	strat := strategy.NewGridStrategy("BTCUSDT", "backtest", decimal.NewFromFloat(1.0), decimal.NewFromFloat(10.0), decimal.NewFromFloat(5.0), 5, 5, 2, 3, false, nil, nil, logger)
	pm := position.NewSuperPositionManager(
		"BTCUSDT", "backtest", 1.0, 10.0, 5.0, 5, 5, 2, 3,
		strat, nil, nil, logger, nil,
	)

	// Initial grid setup
	pm.Initialize(decimal.NewFromInt(45000))

	store := simple.NewMemoryStore()
	engine := simple.NewSimpleEngine(store, pm, orderExecutor, nil, logger)

	runner := NewBacktestRunner(engine, exch)

	// 2. Define test prices
	prices := []decimal.Decimal{
		decimal.NewFromInt(45000),
		decimal.NewFromInt(44999), // Should fill first buy
		decimal.NewFromInt(44998), // Should fill second buy
		decimal.NewFromInt(45001), // Should fill sell
	}

	// 3. Run backtest
	ctx := context.Background()
	exch.StartOrderStream(ctx, func(update *pb.OrderUpdate) {
		engine.OnOrderUpdate(ctx, update)
	})

	err := runner.Run(ctx, "BTCUSDT", prices)
	if err != nil {
		t.Fatalf("Backtest failed: %v", err)
	}

	// 4. Verify results
	// Wait for async processing
	// We expect at least some orders to be filled.

	timeout := time.After(2 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Timed out waiting for filled orders")
		case <-ticker.C:
			filledCount := 0
			allOrders := exch.GetOrders()
			for _, o := range allOrders {
				if o.Status == pb.OrderStatus_ORDER_STATUS_FILLED {
					filledCount++
				}
			}
			if filledCount > 0 {
				t.Logf("Total filled orders: %d", filledCount)
				return // Success
			}
		}
	}
}

func TestBacktest_DynamicGrid(t *testing.T) {
	setupTelemetry()

	// 1. Setup simulated components
	exch := NewSimulatedExchange()
	logger, _ := logging.NewZapLogger("DEBUG")

	orderExecutor := order.NewOrderExecutor(exch, logger)
	orderExecutor.SetRateLimit(1000000, 1000000)

	// BuyWindow=5, SellWindow=5, Interval=10
	strat := strategy.NewGridStrategy("BTCUSDT", "backtest", decimal.NewFromFloat(10.0), decimal.NewFromFloat(100.0), decimal.NewFromFloat(5.0), 5, 5, 2, 3, false, nil, nil, logger)
	pm := position.NewSuperPositionManager(
		"BTCUSDT", "backtest", 10.0, 100.0, 5.0, 5, 5, 2, 3,
		strat, nil, nil, logger, nil,
	)

	// Initial grid setup at 45000
	pm.Initialize(decimal.NewFromInt(45000))

	store := simple.NewMemoryStore()
	engine := simple.NewSimpleEngine(store, pm, orderExecutor, nil, logger)
	runner := NewBacktestRunner(engine, exch)

	// 2. Define test prices: Move price from 45000 -> 45200 (20 intervals)
	// This should trigger the dynamic grid to shift up and place new orders.
	var prices []decimal.Decimal
	start := 45000
	end := 45200
	step := 5 // Smaller steps to trigger updates

	for p := start; p <= end; p += step {
		prices = append(prices, decimal.NewFromInt(int64(p)))
	}

	// 3. Run backtest
	ctx := context.Background()
	exch.StartOrderStream(ctx, func(update *pb.OrderUpdate) {
		engine.OnOrderUpdate(ctx, update)
	})

	err := runner.Run(ctx, "BTCUSDT", prices)
	if err != nil {
		t.Fatalf("Backtest failed: %v", err)
	}

	// 4. Verify new orders created at higher levels
	// With dynamic grid, as price moves to 45200, we should see orders around 45150-45190 (Buy Window)

	// Wait for async processing
	time.Sleep(500 * time.Millisecond)

	slots := pm.GetSlots()
	foundHighSlot := false

	// Check for a slot near the new price (e.g. 45190)
	targetPrice := decimal.NewFromInt(45190)

	// Dump slots for debugging
	for k, v := range slots {
		slotPrice, _ := decimal.NewFromString(k)
		t.Logf("Slot: %s, Status: %s, OrderId: %d", k, v.SlotStatus, v.OrderId)
		if slotPrice.Equal(targetPrice) {
			foundHighSlot = true
		}
	}

	if !foundHighSlot {
		t.Errorf("Dynamic Grid failed: No slot found at %s after price moved to 45200", targetPrice)
	}
}
