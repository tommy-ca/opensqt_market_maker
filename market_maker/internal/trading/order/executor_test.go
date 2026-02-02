package order

import (
	"context"
	"errors"
	"market_maker/internal/core"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestOrderExecutor_PlaceOrder(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	logger := &mockLogger{}
	executor := NewOrderExecutor(exchange, logger)

	req := &pb.PlaceOrderRequest{
		Symbol: "BTCUSDT", Side: pb.OrderSide_ORDER_SIDE_BUY, Type: pb.OrderType_ORDER_TYPE_LIMIT,
		Quantity:      pbu.FromGoDecimal(decimal.NewFromFloat(30.0)),
		Price:         pbu.FromGoDecimal(decimal.NewFromFloat(45000.0)),
		ClientOrderId: "test_order_1",
	}

	ctx := context.Background()
	order, err := executor.PlaceOrder(ctx, req)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if order == nil || order.Symbol != req.Symbol {
		t.Error("Wrong order returned")
	}
}

func TestOrderExecutor_BatchPlaceOrders(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	logger := &mockLogger{}
	executor := NewOrderExecutor(exchange, logger)

	orders := []*pb.PlaceOrderRequest{
		{
			Symbol: "BTCUSDT", Side: pb.OrderSide_ORDER_SIDE_BUY,
			Quantity: pbu.FromGoDecimal(decimal.NewFromFloat(30.0)),
		},
		{
			Symbol: "BTCUSDT", Side: pb.OrderSide_ORDER_SIDE_SELL,
			Quantity: pbu.FromGoDecimal(decimal.NewFromFloat(30.0)),
		},
	}

	ctx := context.Background()
	results, _ := executor.BatchPlaceOrders(ctx, orders)
	if len(results) != 2 {
		t.Errorf("Expected 2, got %d", len(results))
	}
}

func TestOrderExecutor_PostOnlyDegradation(t *testing.T) {
	failingExchange := &mockFailingExchange{
		MockExchange: *mock.NewMockExchange("failing"), failPostOnly: true,
	}
	logger := &mockLogger{}
	executor := NewOrderExecutor(failingExchange, logger)

	req := &pb.PlaceOrderRequest{
		Symbol: "BTCUSDT", Side: pb.OrderSide_ORDER_SIDE_BUY,
		Quantity: pbu.FromGoDecimal(decimal.NewFromFloat(30.0)),
		PostOnly: true,
	}

	ctx := context.Background()
	order, err := executor.PlaceOrder(ctx, req)
	if err != nil {
		t.Fatalf("Failed degradation: %v", err)
	}
	if order == nil {
		t.Fatal("Order nil")
	}
}

func TestOrderExecutor_RetryLogic(t *testing.T) {
	retryExchange := &mockRetryExchange{
		MockExchange: *mock.NewMockExchange("retry"), failCount: 2,
	}
	logger := &mockLogger{}
	executor := NewOrderExecutor(retryExchange, logger)

	req := &pb.PlaceOrderRequest{
		Symbol: "BTCUSDT", Side: pb.OrderSide_ORDER_SIDE_BUY,
		Quantity: pbu.FromGoDecimal(decimal.NewFromFloat(30.0)),
	}
	ctx := context.Background()
	start := time.Now()
	_, err := executor.PlaceOrder(ctx, req)
	if err != nil {
		t.Fatalf("Failed retry: %v", err)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("Too fast")
	}
}

func TestOrderExecutor_NoRetryErrors(t *testing.T) {
	// Should NOT retry margin errors
	marginExchange := &mockRetryExchange{
		MockExchange: *mock.NewMockExchange("margin"), failCount: 5,
		errorMsg: "insufficient margin",
	}
	logger := &mockLogger{}
	executor := NewOrderExecutor(marginExchange, logger)

	req := &pb.PlaceOrderRequest{Symbol: "BTCUSDT"}

	start := time.Now()
	_, err := executor.PlaceOrder(context.Background(), req)

	if err == nil {
		t.Fatal("Expected error")
	}
	// Should fail immediately without retry delay
	if time.Since(start) > 200*time.Millisecond {
		t.Error("Should fail immediately on margin error, but it retried")
	}
}

func TestOrderExecutor_BatchCancelOrders(t *testing.T) {
	exchange := mock.NewMockExchange("test")
	logger := &mockLogger{}
	executor := NewOrderExecutor(exchange, logger)

	ctx := context.Background()
	order1, _ := executor.PlaceOrder(ctx, &pb.PlaceOrderRequest{
		Symbol: "BTCUSDT", Quantity: pbu.FromGoDecimal(decimal.NewFromInt(1)),
	})
	order2, _ := executor.PlaceOrder(ctx, &pb.PlaceOrderRequest{
		Symbol: "BTCUSDT", Quantity: pbu.FromGoDecimal(decimal.NewFromInt(1)),
	})

	err := executor.BatchCancelOrders(ctx, "BTCUSDT", []int64{order1.OrderId, order2.OrderId}, false)
	if err != nil {
		t.Fatalf("Failed cancel: %v", err)
	}
}

type mockFailingExchange struct {
	mock.MockExchange
	failPostOnly bool
}

func (m *mockFailingExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	if m.failPostOnly && req.PostOnly {
		return nil, errors.New("POST_ONLY failure")
	}
	return m.MockExchange.PlaceOrder(ctx, req)
}

type mockRetryExchange struct {
	mock.MockExchange
	failCount int
	current   int
	errorMsg  string
}

func (m *mockRetryExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	if m.current < m.failCount {
		m.current++
		msg := m.errorMsg
		if msg == "" {
			msg = "retry me"
		}
		return nil, errors.New(msg)
	}
	return m.MockExchange.PlaceOrder(ctx, req)
}

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, f ...interface{})               {}
func (m *mockLogger) Info(msg string, f ...interface{})                {}
func (m *mockLogger) Warn(msg string, f ...interface{})                {}
func (m *mockLogger) Error(msg string, f ...interface{})               {}
func (m *mockLogger) Fatal(msg string, f ...interface{})               {}
func (m *mockLogger) WithField(k string, v interface{}) core.ILogger   { return m }
func (m *mockLogger) WithFields(f map[string]interface{}) core.ILogger { return m }
