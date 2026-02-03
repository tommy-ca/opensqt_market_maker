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
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

const (
	testDB = "e2e_test.db"
	symbol = "BTCUSDT"
)

func init() {
	// Setup telemetry for tests
	if _, err := telemetry.Setup("test"); err != nil {
		panic(err)
	}
}

func setupEngine(t *testing.T, exch core.IExchange, dbPath string) (*simple.SimpleEngine, *position.SuperPositionManager, *risk.RiskMonitor) {
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

	gridStrategy := grid.NewGridStrategy(grid.StrategyConfig{
		Symbol:              symbol,
		Exchange:            exch.GetName(),
		PriceInterval:       decimal.NewFromFloat(cfg.Trading.PriceInterval),
		OrderQuantity:       decimal.NewFromFloat(cfg.Trading.OrderQuantity),
		MinOrderValue:       decimal.NewFromFloat(cfg.Trading.MinOrderValue),
		BuyWindowSize:       5,
		SellWindowSize:      5,
		PriceDecimals:       2,
		QtyDecimals:         3,
		IsNeutral:           false,
		VolatilityScale:     cfg.Trading.VolatilityScale,
		InventorySkewFactor: cfg.Trading.InventorySkewFactor,
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

	return engine.(*simple.SimpleEngine), pm, riskMonitor
}

func TestE2E_CrashRecovery(t *testing.T) {
	os.Remove(testDB)
	defer os.Remove(testDB)

	exch := backtest.NewSimulatedExchange()
	ctx := context.Background()

	// 1. Initial Start
	engine, pm, _ := setupEngine(t, exch, testDB)
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}

	initialPrice := decimal.NewFromInt(45000)
	pm.Initialize(initialPrice)

	// Simulate some orders being placed
	err := engine.OnPriceUpdate(ctx, &pb.PriceChange{
		Symbol: symbol,
		Price:  pbu.FromGoDecimal(initialPrice),
	})
	if err != nil {
		t.Fatalf("Price update failed: %v", err)
	}

	// Verify we have locked slots (orders placed)
	slotsBefore := pm.GetSlots()
	lockedBefore := 0
	for _, s := range slotsBefore {
		if s.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED {
			lockedBefore++
		}
	}
	if lockedBefore == 0 {
		t.Fatal("No orders placed before crash")
	}

	// 2. simulate CRASH
	engine.Stop()

	// 3. RECOVER
	engineRec, pmRec, _ := setupEngine(t, exch, testDB)
	if err := engineRec.Start(ctx); err != nil {
		t.Fatalf("Recovery start failed: %v", err)
	}

	// Verify slots are restored
	slotsAfter := pmRec.GetSlots()
	lockedAfter := 0
	for _, s := range slotsAfter {
		if s.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED {
			lockedAfter++
		}
	}

	if lockedAfter != lockedBefore {
		t.Errorf("Expected %d locked slots after recovery, got %d", lockedBefore, lockedAfter)
	}
}

func TestE2E_RiskProtection(t *testing.T) {
	os.Remove(testDB)
	defer os.Remove(testDB)

	exch := backtest.NewSimulatedExchange()
	ctx := context.Background()

	engine, pm, rm := setupEngine(t, exch, testDB)
	if err := rm.Start(ctx); err != nil {
		t.Fatalf("Failed to start risk monitor: %v", err)
	}
	engine.Start(ctx)
	pm.Initialize(decimal.NewFromInt(45000))

	// Normal state
	engine.OnPriceUpdate(ctx, &pb.PriceChange{Symbol: symbol, Price: pbu.FromGoDecimal(decimal.NewFromInt(45000))})

	// Wait for orders to be placed on exchange
	time.Sleep(100 * time.Millisecond)
	openOrders, _ := exch.GetOpenOrders(ctx, symbol, false)
	if len(openOrders) == 0 {
		t.Fatal("No orders placed")
	}

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

	time.Sleep(50 * time.Millisecond) // Wait for global trigger goroutine

	if !rm.IsTriggered() {
		t.Fatal("Risk monitor did not trigger")
	}

	// Next price update should trigger engine to cancel buys
	engine.OnPriceUpdate(ctx, &pb.PriceChange{Symbol: symbol, Price: pbu.FromGoDecimal(decimal.NewFromInt(39999))})

	time.Sleep(200 * time.Millisecond)

	finalOpenOrders, _ := exch.GetOpenOrders(ctx, symbol, false)
	for _, o := range finalOpenOrders {
		if o.Side == pb.OrderSide_ORDER_SIDE_BUY {
			t.Errorf("Buy order %d still open after risk trigger", o.OrderId)
		}
	}
}

func TestE2E_TradingFlow(t *testing.T) {
	os.Remove(testDB)
	defer os.Remove(testDB)

	exch := backtest.NewSimulatedExchange()
	ctx := context.Background()

	engine, pm, _ := setupEngine(t, exch, testDB)
	engine.Start(ctx)

	// Start order stream to feed engine
	exch.StartOrderStream(ctx, func(update *pb.OrderUpdate) {
		engine.OnOrderUpdate(ctx, update)
	})

	initialPrice := decimal.NewFromInt(45000)
	pm.Initialize(initialPrice)

	// 1. First price update - places orders
	engine.OnPriceUpdate(ctx, &pb.PriceChange{Symbol: symbol, Price: pbu.FromGoDecimal(initialPrice)})

	time.Sleep(100 * time.Millisecond)
	openOrders, _ := exch.GetOpenOrders(ctx, symbol, false)
	if len(openOrders) == 0 {
		t.Fatal("No orders placed")
	}

	// 2. Price drop - fill a buy order
	// Level 44990 should have a buy order
	dropPrice := decimal.NewFromInt(44985)
	exch.UpdatePrice(symbol, dropPrice)

	// Wait for fill notification and engine processing
	time.Sleep(200 * time.Millisecond)

	// Verify slot is filled in PM
	slots := pm.GetSlots()
	foundFilled := false
	for _, s := range slots {
		if s.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			foundFilled = true
			break
		}
	}
	if !foundFilled {
		t.Error("No slot marked as FILLED after price drop")
	}

	// 3. Price rise - should place a sell order for the filled slot
	risePrice := decimal.NewFromInt(45010)
	engine.OnPriceUpdate(ctx, &pb.PriceChange{Symbol: symbol, Price: pbu.FromGoDecimal(risePrice)})

	time.Sleep(100 * time.Millisecond)
	openOrders, _ = exch.GetOpenOrders(ctx, symbol, false)
	foundSell := false
	for _, o := range openOrders {
		if o.Side == pb.OrderSide_ORDER_SIDE_SELL {
			foundSell = true
			break
		}
	}
	if !foundSell {
		t.Error("No SELL order placed after repositioning")
	}
}
