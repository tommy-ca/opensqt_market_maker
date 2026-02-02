package risk_test

import (
	"context"
	"github.com/shopspring/decimal"
	"market_maker/internal/core"
	"market_maker/internal/pb"

	"go.opentelemetry.io/otel"
	"market_maker/internal/risk"
	"market_maker/pkg/telemetry"

	"market_maker/internal/trading/position"
	"market_maker/pkg/pbu"
	"sync"
	"testing"
	"time"
)

// MockExchange for the integration test
type MockExchange struct {
	core.IExchange // Embed interface to skip implementing everything
}

func (m *MockExchange) GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error) {
	return []*pb.Order{}, nil
}

func (m *MockExchange) GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error) {
	return []*pb.Position{
		{Symbol: symbol, Size: pbu.FromGoDecimal(decimal.Zero)},
	}, nil
}

func (m *MockExchange) GetName() string { return "mock" }

// MockLogger
type MockLogger struct {
	core.ILogger
}

func (l *MockLogger) Debug(msg string, fields ...interface{})               {}
func (l *MockLogger) Info(msg string, fields ...interface{})                {}
func (l *MockLogger) Warn(msg string, fields ...interface{})                {}
func (l *MockLogger) Error(msg string, fields ...interface{})               {}
func (l *MockLogger) Fatal(msg string, fields ...interface{})               {}
func (l *MockLogger) WithField(key string, value interface{}) core.ILogger  { return l }
func (l *MockLogger) WithFields(fields map[string]interface{}) core.ILogger { return l }

func TestReconciliationRealRace(t *testing.T) {
	// Initialize Telemetry
	meter := otel.GetMeterProvider().Meter("test")
	telemetry.GetGlobalMetrics().InitMetrics(meter)

	// Setup Real Position Manager
	logger := &MockLogger{}
	pm := position.NewSuperPositionManager(
		"BTC-USDT", "binance", 10.0, 0.001, 10.0, 5, 5, 2, 3,
		nil, nil, nil, logger, nil,
	)

	// Initialize with some slots
	err := pm.Initialize(decimal.NewFromInt(50000))
	if err != nil {
		t.Fatalf("Failed to initialize PM: %v", err)
	}

	// Setup Reconciler
	exchange := &MockExchange{}
	reconciler := risk.NewReconciler(exchange, pm, nil, logger, "BTC-USDT", 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// Goroutine 1: Continuous Updates (Write)
	wg.Add(1)
	go func() {
		defer wg.Done()
		update := &pb.OrderUpdate{
			OrderId:     0, // Will correspond to nothing initially, but tests the lock
			Status:      pb.OrderStatus_ORDER_STATUS_FILLED,
			ExecutedQty: pbu.FromGoDecimal(decimal.NewFromInt(1)),
			Side:        pb.OrderSide_ORDER_SIDE_BUY,
		}

		// Find a valid slot to update to make it realistic
		slots := pm.GetSlots()
		var orderID int64 = 12345

		// Hack: inject an order ID into a slot so we can update it
		// We need to do this carefully
		for _, s := range slots {
			s.Mu.Lock()
			s.OrderId = orderID
			s.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED
			s.Mu.Unlock()
			pm.UpdateOrderIndex(orderID, "", s)
			break
		}

		update.OrderId = orderID

		for i := 0; i < 1000; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				pm.OnOrderUpdate(ctx, update)
				time.Sleep(10 * time.Microsecond)
			}
		}
	}()

	// Goroutine 2: Reconciliation (Read)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				reconciler.Reconcile(ctx)
				time.Sleep(100 * time.Microsecond)
			}
		}
	}()

	// Run for a bit
	time.Sleep(2 * time.Second)
	cancel()
	wg.Wait()
}
