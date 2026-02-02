package execution

import (
	"context"
	"errors"
	"market_maker/internal/core"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...interface{})               {}
func (m *mockLogger) Info(msg string, fields ...interface{})                {}
func (m *mockLogger) Warn(msg string, fields ...interface{})                {}
func (m *mockLogger) Error(msg string, fields ...interface{})               {}
func (m *mockLogger) Fatal(msg string, fields ...interface{})               {}
func (m *mockLogger) WithField(key string, value interface{}) core.ILogger  { return m }
func (m *mockLogger) WithFields(fields map[string]interface{}) core.ILogger { return m }

type mockPartialExchange struct {
	mock.MockExchange
	execQty decimal.Decimal
}

func (m *mockPartialExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	order, err := m.MockExchange.PlaceOrder(ctx, req)
	if err == nil && !m.execQty.IsZero() {
		// Only set ExecutedQty for the first leg, not the compensation leg
		if req.Side == pb.OrderSide_ORDER_SIDE_BUY {
			order.ExecutedQty = pbu.FromGoDecimal(m.execQty)
		}
	}
	return order, err
}

type mockFailingExchange struct {
	mock.MockExchange
	fail bool
}

func (m *mockFailingExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	if m.fail {
		return nil, errors.New("failed")
	}
	return m.MockExchange.PlaceOrder(ctx, req)
}

func TestSequenceExecutor_CompensationPartialFill(t *testing.T) {
	spotEx := &mockPartialExchange{
		MockExchange: *mock.NewMockExchange("spot"),
		execQty:      decimal.NewFromFloat(0.6),
	}
	perpEx := &mockFailingExchange{
		MockExchange: *mock.NewMockExchange("perp"),
		fail:         true,
	}
	exchanges := map[string]core.IExchange{"spot": spotEx, "perp": perpEx}

	executor := NewSequenceExecutor(exchanges, &mockLogger{})

	steps := []Step{
		{
			Exchange:   "spot",
			Request:    &pb.PlaceOrderRequest{Symbol: "BTC", Side: pb.OrderSide_ORDER_SIDE_BUY, Quantity: pbu.FromGoDecimal(decimal.NewFromInt(1))},
			Compensate: &pb.PlaceOrderRequest{Symbol: "BTC", Side: pb.OrderSide_ORDER_SIDE_SELL, Quantity: pbu.FromGoDecimal(decimal.NewFromInt(1))},
		},
		{
			Exchange: "perp",
			Request:  &pb.PlaceOrderRequest{Symbol: "BTC", Side: pb.OrderSide_ORDER_SIDE_SELL, Quantity: pbu.FromGoDecimal(decimal.NewFromInt(1))},
		},
	}

	err := executor.Execute(context.Background(), steps)
	require.Error(t, err)

	// Verify spot compensation used 0.6 instead of 1.0
	orders := spotEx.GetOrders()
	// Initial Buy (1.0) + Compensation Sell (0.6)
	require.Len(t, orders, 2)
	assert.Equal(t, pb.OrderSide_ORDER_SIDE_BUY, orders[0].Side)
	assert.Equal(t, "1", pbu.ToGoDecimal(orders[0].Quantity).String())

	assert.Equal(t, pb.OrderSide_ORDER_SIDE_SELL, orders[1].Side)
	assert.Equal(t, "0.6", pbu.ToGoDecimal(orders[1].Quantity).String())
}
