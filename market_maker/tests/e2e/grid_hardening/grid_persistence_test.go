package gridhardening

import (
	"context"
	"testing"
	"time"

	"market_maker/internal/core"
	"market_maker/internal/engine/gridengine"
	"market_maker/internal/engine/simple"
	"market_maker/internal/pb"
	"market_maker/internal/trading/backtest"
	"market_maker/internal/trading/position"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestGridHardening_PersistenceRecovery(t *testing.T) {
	// 1. Setup Shared Dependencies (Store + Exchange)
	store := simple.NewMemoryStore()
	exchange := backtest.NewSimulatedExchange()

	// Helper to spawn engine with shared deps
	spawnEngine := func() *gridengine.GridCoordinator {
		// Logger & Meter
		logger := logging.NewLogger(logging.InfoLevel, nil)
		meter := noop.NewMeterProvider().Meter("test")

		// Mocks
		regimeMonitor := &MockRegimeMonitor{}
		regimeMonitor.SetRegime(pb.MarketRegime_MARKET_REGIME_RANGE)

		executor := &SimulatedExecutor{}
		executor.SetExchange(exchange)

		// Config
		stratCfg := gridengine.Config{
			Symbol:         "BTCUSDT",
			PriceInterval:  decimal.NewFromInt(100),
			OrderQuantity:  decimal.NewFromFloat(0.001),
			BuyWindowSize:  5,
			SellWindowSize: 5,
			QtyDecimals:    3,
		}

		// PM
		pm := position.NewSuperPositionManager(
			"BTCUSDT", "binance", 100.0, 0.001, 10.0, 5, 5, 2, 3,
			nil, nil, store, logger, meter,
		)

		deps := gridengine.GridCoordinatorDeps{
			Cfg:         stratCfg,
			Exchanges:   map[string]core.IExchange{"binance": exchange},
			SlotMgr:     pm,
			RiskMonitor: nil,
			Store:       store,
			Logger:      logger,
			Executor:    executor,
		}

		coord := gridengine.NewGridCoordinator(deps)
		coord.SetRegimeMonitor(regimeMonitor)
		return coord
	}

	// 2. Start Engine A
	engA := spawnEngine()
	ctx := context.Background()
	require.NoError(t, engA.Start(ctx))

	// 3. Place Orders
	price := &pb.PriceChange{
		Symbol:    "BTCUSDT",
		Price:     pbu.FromGoDecimal(decimal.NewFromInt(50000)),
		Timestamp: nil,
	}
	require.NoError(t, engA.OnPriceUpdate(ctx, price))

	assert.Eventually(t, func() bool {
		orders, _ := exchange.GetOpenOrders(ctx, "BTCUSDT", false)
		return len(orders) > 0
	}, 1*time.Second, 10*time.Millisecond, "Orders placed")

	// 4. Crash Engine A
	require.NoError(t, engA.Stop())

	// 5. Ghost Fill (Offline)
	orders, _ := exchange.GetOpenOrders(ctx, "BTCUSDT", false)
	target := orders[0]
	exchange.SimulateOrderFill(target.OrderId, pbu.ToGoDecimal(target.Quantity), pbu.ToGoDecimal(target.Price))

	// 6. Start Engine B
	engB := spawnEngine()
	require.NoError(t, engB.Start(ctx)) // This triggers RestoreState + Reconciliation

	// 7. Verification
	// Engine B should have loaded state from Store AND reconciled with Exchange.
	// The filled order should be marked FILLED in the slot manager.
	// Since we can't inspect internal slot manager easily without accessors,
	// we verify that the slot is NOT Pending/Locked for that order ID anymore.

	// Wait for async reconciliation? Start() does reconciliation synchronously in this impl.

	// We can check if engB places a REPLACEMENT order for the filled slot?
	// Or check equity?

	// Let's verify by triggering another price update.
	// If the slot is correctly filled, the strategy should see it as inventory and maybe place a SELL.
	require.NoError(t, engB.OnPriceUpdate(ctx, price))

	// Verify we have a SELL order corresponding to the filled BUY
	assert.Eventually(t, func() bool {
		orders, _ := exchange.GetOpenOrders(ctx, "BTCUSDT", false)
		for _, o := range orders {
			if o.Side == pb.OrderSide_ORDER_SIDE_SELL {
				return true
			}
		}
		return false
	}, 1*time.Second, 10*time.Millisecond, "Engine B should place SELL order after recovering filled BUY")
}
