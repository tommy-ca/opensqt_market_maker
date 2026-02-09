package e2e

import (
	"context"
	"database/sql"
	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/internal/engine/simple"
	"market_maker/internal/pb"
	"market_maker/internal/risk"
	"market_maker/internal/trading/backtest"
	"market_maker/internal/trading/grid"
	"market_maker/internal/trading/order"
	"market_maker/internal/trading/position"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"market_maker/pkg/telemetry"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/mattn/go-sqlite3"
)

const (
	symbol = "BTCUSDT"
)

func init() {
	// Setup telemetry for tests
	if _, err := telemetry.Setup("test"); err != nil {
		panic(err)
	}
}

func setupEngine(t *testing.T, exch core.IExchange, dbPath string) (*simple.SimpleEngine, *position.SuperPositionManager, *risk.RiskMonitor, func()) {
	logger, _ := logging.NewZapLogger("DEBUG")

	// Config
	cfg := config.DefaultConfig()
	cfg.Trading.Symbol = symbol
	cfg.Trading.PriceInterval = 10.0
	cfg.Trading.OrderQuantity = 100.0

	orderExecutor := order.NewOrderExecutor(exch, logger)

	riskMonitor := risk.NewRiskMonitor(
		exch, logger, []string{symbol}, "1m", 3.0, 20, 2, "All", nil,
	)

	gridStrategy := grid.NewStrategy(grid.StrategyConfig{
		Symbol:              symbol,
		PriceInterval:       decimal.NewFromFloat(cfg.Trading.PriceInterval),
		OrderQuantity:       decimal.NewFromFloat(cfg.Trading.OrderQuantity),
		MinOrderValue:       decimal.NewFromFloat(cfg.Trading.MinOrderValue),
		BuyWindowSize:       5,
		SellWindowSize:      5,
		PriceDecimals:       2,
		QtyDecimals:         3,
		IsNeutral:           false,
		VolatilityScale:     1.0,
		InventorySkewFactor: 0.0,
	})

	pm := position.NewSuperPositionManager(
		symbol, exch.GetName(),
		cfg.Trading.PriceInterval, cfg.Trading.OrderQuantity, cfg.Trading.MinOrderValue,
		5, 5, 2, 3, gridStrategy, riskMonitor, nil, logger, nil,
	)

	// Initialize schema for testing manually since Atlas isn't running
	initDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open db for init: %v", err)
	}
	_, err = initDB.Exec(`
			CREATE TABLE IF NOT EXISTS state (
				id INTEGER PRIMARY KEY CHECK (id = 1),
				data TEXT NOT NULL,
				checksum BLOB NOT NULL,
				updated_at INTEGER NOT NULL
			);
		`)
	initDB.Close()
	if err != nil {
		t.Fatalf("Failed to init schema: %v", err)
	}

	store, err := simple.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	engine := simple.NewSimpleEngine(store, pm, orderExecutor, riskMonitor, logger)

	cleanup := func() {
		store.Close()
	}

	return engine.(*simple.SimpleEngine), pm, riskMonitor, cleanup
}

func TestE2E_CrashRecovery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "e2e_test.db")

	exch := backtest.NewSimulatedExchange()
	ctx := context.Background()

	engine, pm, _, cleanup := setupEngine(t, exch, dbPath)
	defer cleanup()

	_ = engine.Start(ctx)

	// 1. Initial State - place orders
	initialPrice := decimal.NewFromInt(45000)
	_ = pm.Initialize(initialPrice)
	_ = engine.OnPriceUpdate(ctx, &pb.PriceChange{Symbol: symbol, Price: pbu.FromGoDecimal(initialPrice)})

	assert.Eventually(t, func() bool {
		openOrders, _ := exch.GetOpenOrders(ctx, symbol, false)
		return len(openOrders) > 0
	}, 1*time.Second, 10*time.Millisecond, "No orders placed")

	// 2. Stop engine - Simulate Crash
	_ = engine.Stop()

	// 3. Restart engine - Restore State
	engine2, _, _, cleanup2 := setupEngine(t, exch, dbPath)
	defer cleanup2()

	err := engine2.Start(ctx)
	require.NoError(t, err)

	// Verify restored state
	restoredPM := engine2.GetPositionManager()
	if restoredPM.GetSlotCount() == 0 {
		t.Error("Position manager has no slots after restoration")
	}
}

func TestE2E_RiskProtection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "e2e_test.db")

	exch := backtest.NewSimulatedExchange()
	ctx := context.Background()

	engine, pm, rm, cleanup := setupEngine(t, exch, dbPath)
	defer cleanup()
	_ = engine.Start(ctx)

	initialPrice := decimal.NewFromInt(45000)
	_ = pm.Initialize(initialPrice)

	_ = engine.OnPriceUpdate(ctx, &pb.PriceChange{Symbol: symbol, Price: pbu.FromGoDecimal(initialPrice)})

	assert.Eventually(t, func() bool {
		openOrders, _ := exch.GetOpenOrders(ctx, symbol, false)
		return len(openOrders) > 0
	}, 1*time.Second, 10*time.Millisecond, "No orders placed")

	// Trigger Risk Anomaly
	anomalyCandle := &pb.Candle{
		Symbol:   symbol,
		Close:    pbu.FromGoDecimal(decimal.NewFromInt(40000)),
		Volume:   pbu.FromGoDecimal(decimal.NewFromInt(1000000)), // High volume spike
		IsClosed: true,
	}

	// We need some history for RiskMonitor to detect spike
	for i := 0; i < 21; i++ {
		rm.HandleKlineUpdate(&pb.Candle{
			Symbol:   symbol,
			Close:    pbu.FromGoDecimal(decimal.NewFromInt(45000)),
			Volume:   pbu.FromGoDecimal(decimal.NewFromInt(100)),
			IsClosed: true,
		})
	}
	rm.HandleKlineUpdate(anomalyCandle)

	assert.Eventually(t, func() bool {
		return rm.IsTriggered()
	}, 1*time.Second, 10*time.Millisecond, "Risk monitor did not trigger")

	// Next price update should trigger engine to cancel buys
	_ = engine.OnPriceUpdate(ctx, &pb.PriceChange{Symbol: symbol, Price: pbu.FromGoDecimal(decimal.NewFromInt(39999))})

	assert.Eventually(t, func() bool {
		finalOpenOrders, _ := exch.GetOpenOrders(ctx, symbol, false)
		for _, o := range finalOpenOrders {
			if o.Side == pb.OrderSide_ORDER_SIDE_BUY {
				return false
			}
		}
		return true
	}, 1*time.Second, 10*time.Millisecond, "Buy orders still open after risk trigger")
}

func TestE2E_TradingFlow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "e2e_test.db")

	exch := backtest.NewSimulatedExchange()
	ctx := context.Background()

	engine, pm, _, cleanup := setupEngine(t, exch, dbPath)
	defer cleanup()
	_ = engine.Start(ctx)

	// Start order stream to feed engine
	_ = exch.StartOrderStream(ctx, func(update *pb.OrderUpdate) {
		_ = engine.OnOrderUpdate(ctx, update)
	})

	initialPrice := decimal.NewFromInt(45000)
	_ = pm.Initialize(initialPrice)

	// 1. First price update - places orders
	_ = engine.OnPriceUpdate(ctx, &pb.PriceChange{Symbol: symbol, Price: pbu.FromGoDecimal(initialPrice)})

	assert.Eventually(t, func() bool {
		openOrders, _ := exch.GetOpenOrders(ctx, symbol, false)
		return len(openOrders) > 0
	}, 1*time.Second, 10*time.Millisecond, "No orders placed")

	// 2. Price drop - fill a buy order
	// Level 44990 should have a buy order
	dropPrice := decimal.NewFromInt(44985)
	exch.UpdatePrice(symbol, dropPrice)

	// Wait for fill notification and engine processing
	assert.Eventually(t, func() bool {
		slots := pm.GetSlots()
		for _, s := range slots {
			if s.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
				return true
			}
		}
		return false
	}, 1*time.Second, 10*time.Millisecond, "No slot marked as FILLED after price drop")

	// 3. Price rise - should place a sell order for the filled slot
	risePrice := decimal.NewFromInt(45010)
	_ = engine.OnPriceUpdate(ctx, &pb.PriceChange{Symbol: symbol, Price: pbu.FromGoDecimal(risePrice)})

	assert.Eventually(t, func() bool {
		allOrders := exch.GetOrders()
		for _, o := range allOrders {
			if o.Side == pb.OrderSide_ORDER_SIDE_SELL {
				return true
			}
		}
		return false
	}, 1*time.Second, 10*time.Millisecond, "No SELL order placed after repositioning")
}
