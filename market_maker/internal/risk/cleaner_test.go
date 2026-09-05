package risk

import (
	"context"
	"fmt"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestOrderCleaner_Cleanup(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	executor := &mockOrderExecutor{}
	logger := &mockLogger{}

	cleaner := NewOrderCleaner(
		exchange, executor, logger, "BTCUSDT",
		time.Minute,
		5,              // Max open orders
		10*time.Minute, // Max age
	)

	// Setup: Create 10 orders (5 excess)
	for i := 0; i < 10; i++ {
		req := &pb.PlaceOrderRequest{
			Symbol:        "BTCUSDT",
			Side:          pb.OrderSide_ORDER_SIDE_BUY,
			Type:          pb.OrderType_ORDER_TYPE_LIMIT,
			Quantity:      pbu.FromGoDecimal(decimal.NewFromFloat(1.0)),
			Price:         pbu.FromGoDecimal(decimal.NewFromFloat(45000.0)),
			ClientOrderId: fmt.Sprintf("test_%d", i),
		}
		_, _ = exchange.PlaceOrder(context.Background(), req)
	}

	ctx := context.Background()
	err := cleaner.Cleanup(ctx)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Check that 5 orders were cancelled
	if len(executor.cancelledIDs) != 5 {
		t.Errorf("Expected 5 cancelled orders, got %d", len(executor.cancelledIDs))
	}
}

func TestOrderCleaner_Cleanup_Strategy(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	executor := &mockOrderExecutor{}
	logger := &mockLogger{}

	// Max 4 orders allowed
	cleaner := NewOrderCleaner(
		exchange, executor, logger, "BTCUSDT",
		time.Minute,
		4,              // Max open orders
		10*time.Minute, // Max age
	)

	ctx := context.Background()

	// Setup orders:
	// 3 Buys: 40k, 41k, 42k
	// 3 Sells: 48k, 49k, 50k
	// Total 6. Excess 2. Should cancel 1 Buy (40k) and 1 Sell (50k).

	prices := []float64{40000, 41000, 42000, 48000, 49000, 50000}
	sides := []pb.OrderSide{
		pb.OrderSide_ORDER_SIDE_BUY, pb.OrderSide_ORDER_SIDE_BUY, pb.OrderSide_ORDER_SIDE_BUY,
		pb.OrderSide_ORDER_SIDE_SELL, pb.OrderSide_ORDER_SIDE_SELL, pb.OrderSide_ORDER_SIDE_SELL,
	}

	for i, price := range prices {
		req := &pb.PlaceOrderRequest{
			Symbol:        "BTCUSDT",
			Side:          sides[i],
			Type:          pb.OrderType_ORDER_TYPE_LIMIT,
			Quantity:      pbu.FromGoDecimal(decimal.NewFromFloat(1.0)),
			Price:         pbu.FromGoDecimal(decimal.NewFromFloat(price)),
			ClientOrderId: fmt.Sprintf("order_%d", i),
		}
		_, _ = exchange.PlaceOrder(ctx, req)
	}

	// Fetch orders to get assigned IDs
	openOrders, _ := exchange.GetOpenOrders(ctx, "BTCUSDT", false)
	idMap := make(map[float64]int64)
	for _, o := range openOrders {
		p, _ := pbu.ToGoDecimal(o.Price).Float64()
		idMap[p] = o.OrderId
	}

	err := cleaner.Cleanup(ctx)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Verify cancellation
	if len(executor.cancelledIDs) != 2 {
		t.Errorf("Expected 2 cancelled orders, got %d", len(executor.cancelledIDs))
	}

	// Check if correct IDs were cancelled
	cancelledSet := make(map[int64]bool)
	for _, id := range executor.cancelledIDs {
		cancelledSet[id] = true
	}

	if !cancelledSet[idMap[40000]] {
		t.Error("Order at 40000 (lowest buy) should have been cancelled")
	}
	if !cancelledSet[idMap[50000]] {
		t.Error("Order at 50000 (highest sell) should have been cancelled")
	}
}

func TestOrderCleaner_Cleanup_Balance(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	executor := &mockOrderExecutor{}
	logger := &mockLogger{}

	// Max 4 orders
	cleaner := NewOrderCleaner(
		exchange, executor, logger, "BTCUSDT",
		time.Minute,
		4,
		10*time.Minute,
	)

	ctx := context.Background()

	// Buys: 40k, 41k, 42k, 43k, 44k (5 buys)
	// Sell: 50k (1 sell)
	// Total 6. Excess 2.

	// Create orders and map prices to IDs
	idMap := make(map[float64]int64)

	for i := 0; i < 5; i++ {
		price := float64(40000 + i*1000)
		_, _ = exchange.PlaceOrder(ctx, &pb.PlaceOrderRequest{
			Symbol: "BTCUSDT", Side: pb.OrderSide_ORDER_SIDE_BUY,
			Price:    pbu.FromGoDecimal(decimal.NewFromFloat(price)),
			Quantity: pbu.FromGoDecimal(decimal.NewFromFloat(1)),
		})
	}
	_, _ = exchange.PlaceOrder(ctx, &pb.PlaceOrderRequest{
		Symbol: "BTCUSDT", Side: pb.OrderSide_ORDER_SIDE_SELL,
		Price:    pbu.FromGoDecimal(decimal.NewFromFloat(50000)),
		Quantity: pbu.FromGoDecimal(decimal.NewFromFloat(1)),
	})

	// Fetch IDs
	orders, _ := exchange.GetOpenOrders(ctx, "BTCUSDT", false)
	for _, o := range orders {
		p, _ := pbu.ToGoDecimal(o.Price).Float64()
		idMap[p] = o.OrderId
	}

	err := cleaner.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Should remove 2 Buys (lowest: 40k, 41k).
	if len(executor.cancelledIDs) != 2 {
		t.Fatalf("Expected 2 cancelled, got %d", len(executor.cancelledIDs))
	}

	cancelledSet := make(map[int64]bool)
	for _, id := range executor.cancelledIDs {
		cancelledSet[id] = true
	}

	if !cancelledSet[idMap[40000]] {
		t.Error("Buy at 40000 should be cancelled")
	}
	if !cancelledSet[idMap[41000]] {
		t.Error("Buy at 41000 should be cancelled")
	}
	if cancelledSet[idMap[50000]] {
		t.Error("Sell at 50000 should NOT be cancelled")
	}
}

type mockOrderExecutor struct {
	cancelledIDs []int64
}

func (m *mockOrderExecutor) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	return nil, nil
}
func (m *mockOrderExecutor) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	return nil, false
}
func (m *mockOrderExecutor) BatchCancelOrders(ctx context.Context, symbol string, orderIds []int64, useMargin bool) error {
	m.cancelledIDs = append(m.cancelledIDs, orderIds...)
	return nil
}
