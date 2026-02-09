package backtest

import (
	"context"
	"market_maker/internal/pb"
	"market_maker/internal/trading/order"
	"market_maker/internal/trading/position"
	"market_maker/internal/trading/strategy"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"market_maker/pkg/telemetry"
	"testing"
	"time"

	simple "market_maker/internal/engine/simple"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.opentelemetry.io/otel"
)

func setupTelemetry() {
	// Initialize global metrics with default (No-Op) meter if not already initialized
	// This prevents panics in PositionManager which expects metrics to be non-nil
	meter := otel.GetMeterProvider().Meter("backtest")
	_ = telemetry.GetGlobalMetrics().InitMetrics(meter)
}

// MockRiskMonitor for Backtest
type MockRiskMonitorBacktest struct {
	mock.Mock
}

func (m *MockRiskMonitorBacktest) Start(ctx context.Context) error           { return nil }
func (m *MockRiskMonitorBacktest) Stop() error                               { return nil }
func (m *MockRiskMonitorBacktest) IsTriggered() bool                         { return false }
func (m *MockRiskMonitorBacktest) GetVolatilityFactor(symbol string) float64 { return 0 }
func (m *MockRiskMonitorBacktest) CheckHealth() error                        { return nil }
func (m *MockRiskMonitorBacktest) GetATR(symbol string) decimal.Decimal {
	args := m.Called(symbol)
	return args.Get(0).(decimal.Decimal)
}
func (m *MockRiskMonitorBacktest) GetAllSymbols() []string { return nil }
func (m *MockRiskMonitorBacktest) GetMetrics(symbol string) *pb.SymbolRiskMetrics {
	return nil
}
func (m *MockRiskMonitorBacktest) Reset() error { return nil }

func TestBacktest_DynamicInterval_E2E(t *testing.T) {
	setupTelemetry()

	// 1. Setup
	exch := NewSimulatedExchange()
	logger, _ := logging.NewZapLogger("DEBUG")
	rm := &MockRiskMonitorBacktest{}

	orderExecutor := order.NewOrderExecutor(exch, logger)
	orderExecutor.SetRateLimit(1000000, 1000000)

	// Base Interval 10.0
	strat := strategy.NewGridStrategy("BTCUSDT", "backtest",
		decimal.NewFromFloat(10.0), decimal.NewFromFloat(1.0), decimal.NewFromFloat(5.0),
		5, 5, 2, 3, false, rm, nil, logger)

	// Enable Dynamic Interval
	strat.SetDynamicInterval(true, 1.0)

	pm := position.NewSuperPositionManager(
		"BTCUSDT", "backtest", 10.0, 1.0, 5.0, 5, 5, 2, 3,
		strat, rm, nil, logger, nil,
	)

	store := simple.NewMemoryStore()
	engine := simple.NewSimpleEngine(store, pm, orderExecutor, nil, logger)
	runner := NewBacktestRunner(engine, exch)

	// Initialize
	_ = pm.Initialize(decimal.NewFromInt(50000))

	// Wire up updates
	ctx := context.Background()
	_ = exch.StartOrderStream(ctx, func(update *pb.OrderUpdate) {
		_ = engine.OnOrderUpdate(ctx, update)
	})

	// 2. Phase 1: Low Volatility (ATR = 5.0) -> Effective Interval 10.0 (Base)
	rm.On("GetATR", "BTCUSDT").Return(decimal.NewFromFloat(5.0))

	// Run price stable to generate orders
	_ = runner.Run(ctx, "BTCUSDT", []decimal.Decimal{decimal.NewFromInt(50000)})
	time.Sleep(100 * time.Millisecond)

	// Check orders. Should be at 49990, 49980...
	_, _ = exch.GetOpenOrders(ctx, "BTCUSDT", false) // MockExchange method

	slots := pm.GetSlots()
	var buyPrices []decimal.Decimal
	for _, s := range slots {
		if s.OrderSide == pb.OrderSide_ORDER_SIDE_BUY && s.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED {
			buyPrices = append(buyPrices, pbu.ToGoDecimal(s.OrderPrice))
		}
	}

	assert.Contains(t, buyPrices, decimal.NewFromInt(49990), "Should have order at 49990 (Interval 10)")

	// 3. Phase 2: High Volatility (ATR = 50.0) -> Effective Interval 50.0
	// We need to trigger an update. Moving the price slightly will trigger CalculateAdjustments.
	rm.ExpectedCalls = nil // Clear expectations
	rm.On("GetATR", "BTCUSDT").Return(decimal.NewFromFloat(50.0))

	_ = runner.Run(ctx, "BTCUSDT", []decimal.Decimal{decimal.NewFromInt(50001)})
	time.Sleep(100 * time.Millisecond)

	// Check new orders. Should be at 50000 - 50 = 49950?
	// Note: Existing orders at 49990 might remain if logic doesn't cancel them?
	// GridStrategy cancels orders if they are "out of window" or if price slot requires update?
	// calculateSlotAdjustment checks:
	// if LOCKED:
	//   minPrice := current - interval * window
	//   if orderPrice < minPrice || orderPrice > current -> Cancel
	// Since interval increased to 50, window is huge (50*5 = 250). MinPrice = 50000 - 250 = 49750.
	// 49990 is within [49750, 50000]. So it might NOT be canceled!

	// BUT, is 49990 a valid grid level with interval 50?
	// Grid levels are anchored.
	// Nearest(50000, 50000, 50) -> 50000.
	// Levels: 49950, 49900...
	// 49990 is NOT a valid level.
	// Does Strategy check validity of EXISTING orders against new grid levels?
	// Code:
	// Iterate all existing slots.
	// activeBuySlots = map of NEW calculated levels.
	// isBuyCandidate = activeBuySlots[slotPriceStr]
	// if !isBuyCandidate && LOCKED -> Does it cancel?
	// Look at calculateSlotAdjustment:
	// case LOCKED:
	//   It ONLY checks if order is OUT of BuyWindow (price range).
	//   It does NOT check if it aligns with current grid interval!
	//   This might be a logic gap in GridStrategy or intended behavior (let old orders fill).

	// However, new orders should definitely be at new levels.
	// Let's check if 49950 is placed.

	slots = pm.GetSlots()
	found49950 := false
	for _, s := range slots {
		if s.OrderSide == pb.OrderSide_ORDER_SIDE_BUY && s.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED {
			if pbu.ToGoDecimal(s.OrderPrice).Equal(decimal.NewFromInt(49950)) {
				found49950 = true
			}
		}
	}
	assert.True(t, found49950, "Should have order at 49950 (Interval 50)")
}

func TestBacktest_TrendFollowing_E2E(t *testing.T) {
	setupTelemetry()

	// 1. Setup
	exch := NewSimulatedExchange()
	logger, _ := logging.NewZapLogger("DEBUG")
	rm := &MockRiskMonitorBacktest{}

	orderExecutor := order.NewOrderExecutor(exch, logger)
	orderExecutor.SetRateLimit(1000000, 1000000)

	// Base Interval 10.0
	strat := strategy.NewGridStrategy("BTCUSDT", "backtest",
		decimal.NewFromFloat(10.0), decimal.NewFromFloat(1.0), decimal.NewFromFloat(5.0),
		5, 5, 2, 3, false, rm, nil, logger)

	// Enable Trend Following (Skew 0.001 -> 0.1% -> 50 USDT at 50k)
	// This is aggressive: 1 unit of inventory shifts grid down by 50 USDT (5 intervals).
	strat.SetTrendFollowing(0.001)

	pm := position.NewSuperPositionManager(
		"BTCUSDT", "backtest", 10.0, 1.0, 5.0, 5, 5, 2, 3,
		strat, rm, nil, logger, nil,
	)

	store := simple.NewMemoryStore()
	engine := simple.NewSimpleEngine(store, pm, orderExecutor, nil, logger)
	// runner := NewBacktestRunner(engine, exch) // Not used

	_ = pm.Initialize(decimal.NewFromInt(50000))

	ctx := context.Background()
	_ = exch.StartOrderStream(ctx, func(update *pb.OrderUpdate) {
		_ = engine.OnOrderUpdate(ctx, update)
	})

	// 2. Build Inventory Manually (Step-by-Step to ensure fills process)
	// We want to accumulate inventory to trigger skew.

	steps := []decimal.Decimal{
		decimal.NewFromInt(50000),
		decimal.NewFromInt(49990), // Fill 1
		decimal.NewFromInt(49980), // Fill 2
		decimal.NewFromInt(49970), // Fill 3
	}

	for _, p := range steps {
		// Update exchange (triggers fills if matches)
		exch.UpdatePrice("BTCUSDT", p)

		// Wait for fill to be processed
		// Wait logic: check slots for fill status or just wait
		time.Sleep(200 * time.Millisecond)

		// Notify engine to recalculate (triggers strategy)
		_ = engine.OnPriceUpdate(ctx, &pb.PriceChange{
			Symbol: "BTCUSDT",
			Price:  pbu.FromGoDecimal(p),
		})

		// Allow time for orders to be placed
		time.Sleep(200 * time.Millisecond)
	}

	// Verify inventory
	snapshot := pm.GetSnapshot()
	inventory := decimal.Zero
	for _, s := range snapshot.Slots {
		if s.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			inventory = inventory.Add(pbu.ToGoDecimal(s.PositionQty))
		}
	}
	// We expect at least 1 filled order. Note: Trend following might cancel orders
	// near the price as inventory builds, so we might not get all 3 fills.
	assert.True(t, inventory.GreaterThanOrEqual(decimal.NewFromFloat(1.0)), "Should have inventory")

	// 3. Verify Skew
	// Inventory >= 1. Skew Factor 0.001.
	// Skew = 1 * 0.001 = 0.001 (0.1%).
	// Reference price 49970.
	// Skewed Ref = 49970 * (1 - 0.001) = 49920.
	// New Grid center around 49920.
	// Next Buy should be around 49920 - 10 = 49910.
	// Without skew, next buy would be around 49960.

	// Check open buy orders
	slots := pm.GetSlots()
	maxBuyPrice := decimal.Zero

	for _, s := range slots {
		if s.OrderSide == pb.OrderSide_ORDER_SIDE_BUY && s.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED {
			price := pbu.ToGoDecimal(s.OrderPrice)
			if price.GreaterThan(maxBuyPrice) {
				maxBuyPrice = price
			}
			if price.GreaterThanOrEqual(decimal.NewFromInt(49900)) {
				t.Logf("Found high buy order: Price=%s, ID=%d, Status=%s", price, s.OrderId, s.SlotStatus)
			}
		}
	}

	// 49820 reference. Max buy ~49810.

	assert.True(t, maxBuyPrice.LessThan(decimal.NewFromInt(49950)), "Buy orders should be skewed lower than 49950")
}
