package risk

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock objects for testing
type ServiceMockExchange struct {
	mock.Mock
}

func (m *ServiceMockExchange) GetName() string                       { return "mock" }
func (m *ServiceMockExchange) IsUnifiedMargin() bool                 { return false }
func (m *ServiceMockExchange) CheckHealth(ctx context.Context) error { return nil }
func (m *ServiceMockExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error {
	return nil
}
func (m *ServiceMockExchange) StopKlineStream() error { return nil }
func (m *ServiceMockExchange) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error) {
	args := m.Called(ctx, symbol, interval, limit)
	return args.Get(0).([]*pb.Candle), args.Error(1)
}

func (m *ServiceMockExchange) GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error) {
	args := m.Called(ctx, symbol, useMargin)
	return args.Get(0).([]*pb.Order), args.Error(1)
}
func (m *ServiceMockExchange) GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error) {
	args := m.Called(ctx, symbol)
	return args.Get(0).([]*pb.Position), args.Error(1)
}
func (m *ServiceMockExchange) CancelOrder(ctx context.Context, symbol string, orderID int64, useMargin bool) error {
	args := m.Called(ctx, symbol, orderID, useMargin)
	return args.Error(0)
}

// Satisfy other IExchange methods with dummies
func (m *ServiceMockExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	return nil, nil
}
func (m *ServiceMockExchange) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	return nil, false
}
func (m *ServiceMockExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error {
	return nil
}
func (m *ServiceMockExchange) CancelAllOrders(ctx context.Context, symbol string, useMargin bool) error {
	return nil
}
func (m *ServiceMockExchange) GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string, useMargin bool) (*pb.Order, error) {
	return nil, nil
}
func (m *ServiceMockExchange) GetAccount(ctx context.Context) (*pb.Account, error) { return nil, nil }
func (m *ServiceMockExchange) GetBalance(ctx context.Context, asset string) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
func (m *ServiceMockExchange) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
	return nil
}
func (m *ServiceMockExchange) StopOrderStream() error { return nil }
func (m *ServiceMockExchange) StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error {
	return nil
}
func (m *ServiceMockExchange) StartAccountStream(ctx context.Context, callback func(account *pb.Account)) error {
	return nil
}
func (m *ServiceMockExchange) StartPositionStream(ctx context.Context, callback func(position *pb.Position)) error {
	return nil
}
func (m *ServiceMockExchange) GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
func (m *ServiceMockExchange) FetchExchangeInfo(ctx context.Context, symbol string) error { return nil }
func (m *ServiceMockExchange) GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error) {
	return nil, nil
}
func (m *ServiceMockExchange) GetPriceDecimals() int    { return 2 }
func (m *ServiceMockExchange) GetQuantityDecimals() int { return 3 }
func (m *ServiceMockExchange) GetBaseAsset() string     { return "BTC" }
func (m *ServiceMockExchange) GetQuoteAsset() string    { return "USDT" }
func (m *ServiceMockExchange) GetFundingRate(ctx context.Context, symbol string) (*pb.FundingRate, error) {
	return nil, nil
}
func (m *ServiceMockExchange) GetFundingRates(ctx context.Context) ([]*pb.FundingRate, error) {
	return nil, nil
}
func (m *ServiceMockExchange) GetHistoricalFundingRates(ctx context.Context, symbol string, limit int) ([]*pb.FundingRate, error) {
	return nil, nil
}
func (m *ServiceMockExchange) GetTickers(ctx context.Context) ([]*pb.Ticker, error) {
	return nil, nil
}
func (m *ServiceMockExchange) GetOpenInterest(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
func (m *ServiceMockExchange) StartFundingRateStream(ctx context.Context, symbol string, callback func(update *pb.FundingUpdate)) error {
	return nil
}
func (m *ServiceMockExchange) GetSymbols(ctx context.Context) ([]string, error) {
	return []string{"BTCUSDT"}, nil
}

type MockPositionManager struct {
	mock.Mock
}

func (m *MockPositionManager) Initialize(anchorPrice decimal.Decimal) error              { return nil }
func (m *MockPositionManager) RestoreState(slots map[string]*pb.InventorySlot) error     { return nil }
func (m *MockPositionManager) RestoreFromExchangePosition(totalPosition decimal.Decimal) {}
func (m *MockPositionManager) CalculateAdjustments(ctx context.Context, newPrice decimal.Decimal) ([]*pb.OrderAction, error) {
	return nil, nil
}
func (m *MockPositionManager) ApplyActionResults(results []core.OrderActionResult) error { return nil }
func (m *MockPositionManager) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	args := m.Called(ctx, update)
	return args.Error(0)
}
func (m *MockPositionManager) CancelAllBuyOrders(ctx context.Context) ([]*pb.OrderAction, error) {
	return nil, nil
}
func (m *MockPositionManager) CancelAllSellOrders(ctx context.Context) ([]*pb.OrderAction, error) {
	return nil, nil
}
func (m *MockPositionManager) GetSlots() map[string]*core.InventorySlot { return nil }
func (m *MockPositionManager) GetStrategySlots(target []core.StrategySlot) []core.StrategySlot {
	return nil
}
func (m *MockPositionManager) GetSlotCount() int { return 0 }
func (m *MockPositionManager) GetSnapshot() *pb.PositionManagerSnapshot {
	return &pb.PositionManagerSnapshot{Slots: make(map[string]*pb.InventorySlot)}
}
func (m *MockPositionManager) CreateReconciliationSnapshot() map[string]*core.InventorySlot {
	return nil
}
func (m *MockPositionManager) UpdateOrderIndex(orderID int64, clientOID string, slot *core.InventorySlot) {
}
func (m *MockPositionManager) MarkSlotsPending(actions []*pb.OrderAction) {
}
func (m *MockPositionManager) ForceSync(ctx context.Context, symbol string, exchangeSize decimal.Decimal) error {
	return nil
}
func (m *MockPositionManager) OnUpdate(callback func(*pb.PositionUpdate)) {}

func (m *MockPositionManager) GetFills() []*pb.Fill {
	return nil
}

func (m *MockPositionManager) GetOrderHistory() []*pb.Order {
	return nil
}

func (m *MockPositionManager) GetPositionHistory() []*pb.PositionSnapshotData {
	return nil
}

func (m *MockPositionManager) GetRealizedPnL() decimal.Decimal {
	return decimal.Zero
}
func (m *MockPositionManager) SyncOrders(orders []*pb.Order, exchangePosition decimal.Decimal) {}

func TestRiskServiceServer_GetCircuitBreakerStatus(t *testing.T) {
	cb := NewCircuitBreaker(CircuitConfig{
		MaxConsecutiveLosses: 3,
		CooldownPeriod:       1 * time.Hour,
	})

	server := NewRiskServiceServer(nil, nil, cb)

	resp, err := server.GetCircuitBreakerStatus(context.Background(), &pb.GetCircuitBreakerStatusRequest{Symbol: "BTCUSDT"})
	assert.NoError(t, err)
	assert.False(t, resp.Status.IsOpen)

	// Trip it
	_ = cb.Open("BTCUSDT", "test")
	resp, err = server.GetCircuitBreakerStatus(context.Background(), &pb.GetCircuitBreakerStatusRequest{Symbol: "BTCUSDT"})
	assert.NoError(t, err)
	assert.True(t, resp.Status.IsOpen)
}

func TestRiskServiceServer_ControlCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(CircuitConfig{})
	server := NewRiskServiceServer(nil, nil, cb)

	// Manual Open
	_, err := server.OpenCircuitBreaker(context.Background(), &pb.OpenCircuitBreakerRequest{Symbol: "BTCUSDT", Reason: "Manual"})
	assert.NoError(t, err)
	assert.True(t, cb.IsTripped())

	// Manual Close
	_, err = server.CloseCircuitBreaker(context.Background(), &pb.CloseCircuitBreakerRequest{Symbol: "BTCUSDT"})
	assert.NoError(t, err)
	assert.False(t, cb.IsTripped())
}

func TestRiskServiceServer_GetRiskMetrics(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	mockEx := &ServiceMockExchange{}
	mockEx.On("GetHistoricalKlines", mock.Anything, "BTCUSDT", "1m", mock.Anything).Return([]*pb.Candle{}, nil)

	rm := NewRiskMonitor(mockEx, logger, []string{"BTCUSDT"}, "1m", 2.0, 10, 1, "All", nil)
	server := NewRiskServiceServer(rm, nil, nil)

	// Inject some data
	rm.HandleKlineUpdate(&pb.Candle{
		Symbol:   "BTCUSDT",
		Close:    pbu.FromGoDecimal(decimal.NewFromInt(50000)),
		Volume:   pbu.FromGoDecimal(decimal.NewFromInt(100)),
		IsClosed: true,
	})

	resp, err := server.GetRiskMetrics(context.Background(), &pb.GetRiskMetricsRequest{Symbols: []string{"BTCUSDT"}})
	assert.NoError(t, err)
	if assert.NotEmpty(t, resp.Metrics) {
		assert.Equal(t, "BTCUSDT", resp.Metrics[0].Symbol)
	}
}

func TestRiskServiceServer_TriggerReconciliation(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	mockEx := &ServiceMockExchange{}
	mockPm := &MockPositionManager{}

	mockEx.On("GetOpenOrders", mock.Anything, "BTCUSDT", false).Return([]*pb.Order{}, nil)
	mockEx.On("GetPositions", mock.Anything, "BTCUSDT").Return([]*pb.Position{
		{Symbol: "BTCUSDT", Size: pbu.FromGoDecimal(decimal.Zero)},
	}, nil)

	rec := NewReconciler(mockEx, mockPm, nil, logger, "BTCUSDT", time.Hour)
	server := NewRiskServiceServer(nil, rec, nil)

	resp, err := server.TriggerReconciliation(context.Background(), &pb.TriggerReconciliationRequest{Symbol: "BTCUSDT"})
	assert.NoError(t, err)
	assert.Equal(t, "completed", resp.Status)
}
