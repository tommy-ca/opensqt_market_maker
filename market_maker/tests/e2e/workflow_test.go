package e2e

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"market_maker/internal/core"
	"market_maker/internal/engine/gridengine"
	"market_maker/internal/engine/simple"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/internal/risk"
	"market_maker/internal/trading/grid"
	"market_maker/pkg/concurrency"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"

	_ "github.com/mattn/go-sqlite3"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func initTestDB(t *testing.T, dbPath string) {
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	// Initialize schema manually since Atlas isn't running in tests
	// Based on e2e_test.go implementation
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			data TEXT NOT NULL,
			checksum BLOB NOT NULL,
			updated_at INTEGER NOT NULL
		);
	`)
	require.NoError(t, err)
}

func TestE2E_DurableRecovery_OfflineFills(t *testing.T) {
	// 1. Setup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := logging.NewLogger(logging.InfoLevel, nil)
	dbPath := "test_recovery.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	initTestDB(t, dbPath)

	store, err := simple.NewSQLiteStore(dbPath)
	require.NoError(t, err)

	exch := mock.NewMockExchange("mock")
	// Set initial price
	exch.SetTicker(&pb.Ticker{
		Symbol:    "BTCUSDT",
		LastPrice: pbu.FromGoDecimal(decimal.NewFromFloat(100.0)),
	})

	// Setup components for Run 1
	sm1 := grid.NewSlotManager("BTCUSDT", 2, logger)
	cfg := gridengine.Config{
		Symbol:         "BTCUSDT",
		PriceInterval:  decimal.NewFromFloat(1.0),
		OrderQuantity:  decimal.NewFromFloat(1.0),
		BuyWindowSize:  2,
		SellWindowSize: 2,
	}

	execPool := concurrency.NewWorkerPool(concurrency.PoolConfig{
		Name:        "e2e-pool",
		MaxWorkers:  4,
		MaxCapacity: 100,
		IdleTimeout: time.Second,
	}, logger)
	defer execPool.Stop()

	// Initial Engine
	eng1 := gridengine.NewGridEngine(
		map[string]core.IExchange{"mock": exch},
		exch, // MockExchange implements IOrderExecutor
		nil,  // No risk monitor for this test
		store,
		logger,
		execPool,
		sm1,
		cfg,
	)

	// 2. Execution - Run 1
	err = eng1.Start(ctx)
	require.NoError(t, err)

	// Send Price Update to trigger orders
	update1 := &pb.PriceChange{
		Symbol:    "BTCUSDT",
		Price:     pbu.FromGoDecimal(decimal.NewFromFloat(100.0)),
		Timestamp: timestamppb.Now(),
	}
	err = eng1.OnPriceUpdate(ctx, update1)
	require.NoError(t, err)

	// Wait for orders to be placed
	time.Sleep(100 * time.Millisecond)

	orders := exch.GetOrders()
	require.GreaterOrEqual(t, len(orders), 2, "Should have placed orders")

	// Identify a Buy order to fill
	var targetOrder *pb.Order
	for _, o := range orders {
		if o.Side == pb.OrderSide_ORDER_SIDE_BUY {
			targetOrder = o
			break
		}
	}
	require.NotNil(t, targetOrder, "Should have a buy order")

	// Stop Engine 1
	err = eng1.Stop()
	require.NoError(t, err)

	// Close store to release locks/connections
	store.Close()

	// 3. Offline Action
	// Simulate fill while engine is down
	fillQty := pbu.ToGoDecimal(targetOrder.Quantity)
	fillPrice := pbu.ToGoDecimal(targetOrder.Price)
	exch.SimulateOrderFill(targetOrder.OrderId, fillQty, fillPrice)

	// Verify exchange state is filled
	updatedOrder, _ := exch.GetOrder(ctx, "BTCUSDT", targetOrder.OrderId, "", false)
	assert.Equal(t, pb.OrderStatus_ORDER_STATUS_FILLED, updatedOrder.Status)

	// 4. Restart - Run 2
	// Reopen Store
	store2, err := simple.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	defer store2.Close()

	// New SlotManager to ensure we load from DB
	sm2 := grid.NewSlotManager("BTCUSDT", 2, logger)

	eng2 := gridengine.NewGridEngine(
		map[string]core.IExchange{"mock": exch},
		exch,
		nil,
		store2,
		logger,
		execPool,
		sm2,
		cfg,
	)

	err = eng2.Start(ctx)
	require.NoError(t, err)

	// Simulate Reconciliation (Trigger Order Update explicitly as engine restart doesn't auto-fetch yet)
	update := &pb.OrderUpdate{
		OrderId:     targetOrder.OrderId,
		Symbol:      targetOrder.Symbol,
		Status:      pb.OrderStatus_ORDER_STATUS_FILLED,
		ExecutedQty: targetOrder.ExecutedQty,
		AvgPrice:    targetOrder.AvgPrice,
		UpdateTime:  time.Now().UnixMilli(),
	}
	err = eng2.OnOrderUpdate(ctx, update)
	require.NoError(t, err)

	// Check state in New SlotManager
	slots := sm2.GetSlots()
	var filledSlot *core.InventorySlot
	targetPrice := pbu.ToGoDecimal(targetOrder.Price)
	for _, s := range slots {
		if pbu.ToGoDecimal(s.Price).Equal(targetPrice) {
			filledSlot = s
			break
		}
	}
	require.NotNil(t, filledSlot, "Filled slot should exist")

	// Assert: GridEngine.GetSlots() should show the slot as FILLED (or FREE/FILLED depending on logic)
	assert.NotEqual(t, pb.SlotStatus_SLOT_STATUS_LOCKED, filledSlot.SlotStatus, "Slot should not be LOCKED after offline fill")
	assert.Equal(t, pb.PositionStatus_POSITION_STATUS_FILLED, filledSlot.PositionStatus, "Position status should be FILLED")
}

func TestE2E_RiskCircuitBreaker(t *testing.T) {
	// 1. Setup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := logging.NewLogger(logging.InfoLevel, nil)
	dbPath := "test_risk.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	initTestDB(t, dbPath)

	store, err := simple.NewSQLiteStore(dbPath)
	require.NoError(t, err)

	exch := mock.NewMockExchange("mock")

	// Setup History for RiskMonitor
	window := 5
	basePrice := 100.0
	baseVol := 100.0

	rm := risk.NewRiskMonitor(
		exch,
		logger,
		[]string{"BTCUSDT"},
		"1m",
		1.5, // Volume Multiplier
		window,
		1, // Recovery Threshold
		"Any",
		nil,
	)

	sm := grid.NewSlotManager("BTCUSDT", 2, logger)
	cfg := gridengine.Config{
		Symbol:         "BTCUSDT",
		PriceInterval:  decimal.NewFromFloat(1.0),
		OrderQuantity:  decimal.NewFromFloat(1.0),
		BuyWindowSize:  2,
		SellWindowSize: 2,
	}

	execPool := concurrency.NewWorkerPool(concurrency.PoolConfig{
		Name:        "e2e-risk-pool",
		MaxWorkers:  4,
		MaxCapacity: 100,
		IdleTimeout: time.Second,
	}, logger)
	defer execPool.Stop()

	eng := gridengine.NewGridEngine(
		map[string]core.IExchange{"mock": exch},
		exch,
		rm,
		store,
		logger,
		execPool,
		sm,
		cfg,
	)

	// Start Risk Monitor
	err = rm.Start(ctx)
	require.NoError(t, err)

	// Feed history to RiskMonitor
	for i := 0; i < window; i++ {
		rm.HandleKlineUpdate(&pb.Candle{
			Symbol:    "BTCUSDT",
			Close:     pbu.FromGoDecimal(decimal.NewFromFloat(basePrice)),
			Volume:    pbu.FromGoDecimal(decimal.NewFromFloat(baseVol)),
			IsClosed:  true,
			Timestamp: time.Now().Add(-time.Duration(window-i) * time.Minute).UnixMilli(),
		})
	}

	// Start Engine
	err = eng.Start(ctx)
	require.NoError(t, err)

	// 2. Execution
	// Normal Price Update
	updateNormal := &pb.PriceChange{
		Symbol:    "BTCUSDT",
		Price:     pbu.FromGoDecimal(decimal.NewFromFloat(100.0)),
		Timestamp: timestamppb.Now(),
	}
	err = eng.OnPriceUpdate(ctx, updateNormal)
	require.NoError(t, err)

	// Wait and verify normal orders
	time.Sleep(50 * time.Millisecond)
	orders1 := exch.GetOrders()
	assert.NotEmpty(t, orders1)
	lastID := int64(0)
	for _, o := range orders1 {
		if o.OrderId > lastID {
			lastID = o.OrderId
		}
	}

	// 3. Volatility Spike
	spikeCandle := &pb.Candle{
		Symbol:    "BTCUSDT",
		Close:     pbu.FromGoDecimal(decimal.NewFromFloat(95.0)),
		Volume:    pbu.FromGoDecimal(decimal.NewFromFloat(200.0)), // 2x avg (trigger > 1.5x)
		IsClosed:  true,
		Timestamp: time.Now().UnixMilli(),
	}
	rm.HandleKlineUpdate(spikeCandle)

	// Wait for async processing
	time.Sleep(100 * time.Millisecond)

	assert.True(t, rm.IsTriggered(), "Risk Monitor should be triggered")

	// 4. Price Update during Risk Trigger
	updateRisk := &pb.PriceChange{
		Symbol:    "BTCUSDT",
		Price:     pbu.FromGoDecimal(decimal.NewFromFloat(90.0)),
		Timestamp: timestamppb.Now(),
	}
	err = eng.OnPriceUpdate(ctx, updateRisk)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	orders2 := exch.GetOrders()
	// Assert: No *new* Buy orders placed
	for _, o := range orders2 {
		if o.OrderId > lastID {
			assert.NotEqual(t, pb.OrderSide_ORDER_SIDE_BUY, o.Side, "Should not place new BUY orders when risk triggered")
		}
	}
}
