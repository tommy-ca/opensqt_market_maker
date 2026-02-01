package position

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"

	"github.com/shopspring/decimal"
)

// Mock implementations for testing

type mockOrderExecutor struct {
	placedOrders []*pb.Order
	cancelledIDs []int64
}

func (m *mockOrderExecutor) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	order := &pb.Order{
		OrderId: int64(len(m.placedOrders) + 1000), ClientOrderId: req.ClientOrderId,
		Symbol: req.Symbol, Side: req.Side, Status: pb.OrderStatus_ORDER_STATUS_NEW,
	}
	m.placedOrders = append(m.placedOrders, order)
	return order, nil
}

func (m *mockOrderExecutor) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	res := make([]*pb.Order, len(orders))
	for i, r := range orders {
		res[i], _ = m.PlaceOrder(ctx, r)
	}
	return res, false
}

func (m *mockOrderExecutor) BatchCancelOrders(ctx context.Context, symbol string, orderIds []int64, useMargin bool) error {
	m.cancelledIDs = append(m.cancelledIDs, orderIds...)
	return nil
}

type mockRiskMonitor struct {
	triggered bool
	vol       float64
}

func (m *mockRiskMonitor) Start(ctx context.Context) error                { return nil }
func (m *mockRiskMonitor) Stop() error                                    { return nil }
func (m *mockRiskMonitor) IsTriggered() bool                              { return m.triggered }
func (m *mockRiskMonitor) GetVolatilityFactor(symbol string) float64      { return m.vol }
func (m *mockRiskMonitor) CheckHealth() error                             { return nil }
func (m *mockRiskMonitor) GetATR(symbol string) decimal.Decimal           { return decimal.Zero }
func (m *mockRiskMonitor) GetAllSymbols() []string                        { return nil }
func (m *mockRiskMonitor) GetMetrics(symbol string) *pb.SymbolRiskMetrics { return nil }
func (m *mockRiskMonitor) Reset() error                                   { return nil }

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, f ...interface{})               {}
func (m *mockLogger) Info(msg string, f ...interface{})                {}
func (m *mockLogger) Warn(msg string, f ...interface{})                {}
func (m *mockLogger) Error(msg string, f ...interface{})               {}
func (m *mockLogger) Fatal(msg string, f ...interface{})               {}
func (m *mockLogger) WithField(k string, v interface{}) core.ILogger   { return m }
func (m *mockLogger) WithFields(f map[string]interface{}) core.ILogger { return m }
