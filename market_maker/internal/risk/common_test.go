package risk

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
)

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, f ...interface{})               { fmt.Printf("DEBUG: %s %v\n", msg, f) }
func (m *mockLogger) Info(msg string, f ...interface{})                { fmt.Printf("INFO: %s %v\n", msg, f) }
func (m *mockLogger) Warn(msg string, f ...interface{})                { fmt.Printf("WARN: %s %v\n", msg, f) }
func (m *mockLogger) Error(msg string, f ...interface{})               { fmt.Printf("ERROR: %s %v\n", msg, f) }
func (m *mockLogger) Fatal(msg string, f ...interface{})               { fmt.Printf("FATAL: %s %v\n", msg, f) }
func (m *mockLogger) WithField(k string, v interface{}) core.ILogger   { return m }
func (m *mockLogger) WithFields(f map[string]interface{}) core.ILogger { return m }

type mockPositionManager struct {
	mock.Mock
	slots   map[string]*core.InventorySlot
	updates []*pb.OrderUpdate
}

func (m *mockPositionManager) Initialize(anchorPrice decimal.Decimal) error {
	args := m.Called(anchorPrice)
	return args.Error(0)
}
func (m *mockPositionManager) RestoreState(slots map[string]*pb.InventorySlot) error {
	args := m.Called(slots)
	return args.Error(0)
}
func (m *mockPositionManager) CalculateAdjustments(ctx context.Context, newPrice decimal.Decimal) ([]*pb.OrderAction, error) {
	args := m.Called(context.Background(), newPrice)
	return args.Get(0).([]*pb.OrderAction), args.Error(1)
}
func (m *mockPositionManager) ApplyActionResults(results []core.OrderActionResult) error {
	args := m.Called(results)
	return args.Error(0)
}
func (m *mockPositionManager) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	m.updates = append(m.updates, update)
	args := m.Called(context.Background(), update)
	return args.Error(0)
}
func (m *mockPositionManager) CancelAllBuyOrders(ctx context.Context) ([]*pb.OrderAction, error) {
	args := m.Called(context.Background())
	return args.Get(0).([]*pb.OrderAction), args.Error(1)
}
func (m *mockPositionManager) CancelAllSellOrders(ctx context.Context) ([]*pb.OrderAction, error) {
	args := m.Called(context.Background())
	return args.Get(0).([]*pb.OrderAction), args.Error(1)
}
func (m *mockPositionManager) GetSlots() map[string]*core.InventorySlot {
	args := m.Called()
	if args.Get(0) == nil {
		return m.slots
	}
	return args.Get(0).(map[string]*core.InventorySlot)
}
func (m *mockPositionManager) GetSlotCount() int {
	return len(m.slots)
}
func (m *mockPositionManager) GetSnapshot() *pb.PositionManagerSnapshot {
	args := m.Called()
	if args.Get(0) == nil {
		return &pb.PositionManagerSnapshot{Symbol: "BTCUSDT"}
	}
	return args.Get(0).(*pb.PositionManagerSnapshot)
}
func (m *mockPositionManager) CreateReconciliationSnapshot() map[string]*core.InventorySlot {
	args := m.Called()
	if args.Get(0) == nil {
		return m.slots
	}
	return args.Get(0).(map[string]*core.InventorySlot)
}
func (m *mockPositionManager) UpdateOrderIndex(orderID int64, clientOID string, slot *core.InventorySlot) {
	m.Called(orderID, clientOID, slot)
}
func (m *mockPositionManager) ForceSync(ctx context.Context, symbol string, exchangeSize decimal.Decimal) error {
	args := m.Called(context.Background(), symbol, exchangeSize)
	return args.Error(0)
}
func (m *mockPositionManager) RestoreFromExchangePosition(totalPosition decimal.Decimal) {
	m.Called(totalPosition)
}
func (m *mockPositionManager) OnUpdate(callback func(*pb.PositionUpdate)) {
	m.Called(callback)
}
func (m *mockPositionManager) GetFills() []*pb.Fill {
	return nil
}
func (m *mockPositionManager) GetOrderHistory() []*pb.Order {
	return nil
}
func (m *mockPositionManager) GetPositionHistory() []*pb.PositionSnapshotData {
	return nil
}
func (m *mockPositionManager) GetRealizedPnL() decimal.Decimal {
	return decimal.Zero
}

type MockExchange struct {
	mock.Mock
}

func (m *MockExchange) GetName() string                       { return "mock" }
func (m *MockExchange) IsUnifiedMargin() bool                 { return false }
func (m *MockExchange) CheckHealth(ctx context.Context) error { return nil }
func (m *MockExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	args := m.Called(context.Background(), req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.Order), args.Error(1)
}
func (m *MockExchange) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	return nil, false
}

func (m *MockExchange) CancelOrder(ctx context.Context, symbol string, orderID int64, useMargin bool) error {
	args := m.Called(context.Background(), symbol, orderID, useMargin)
	return args.Error(0)
}

func (m *MockExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error {
	return nil
}

func (m *MockExchange) CancelAllOrders(ctx context.Context, symbol string, useMargin bool) error {
	return nil
}
func (m *MockExchange) GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string, useMargin bool) (*pb.Order, error) {
	return nil, nil
}

func (m *MockExchange) GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error) {
	args := m.Called(context.Background(), symbol, useMargin)
	return args.Get(0).([]*pb.Order), args.Error(1)
}
func (m *MockExchange) GetAccount(ctx context.Context) (*pb.Account, error) { return nil, nil }
func (m *MockExchange) GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error) {
	args := m.Called(context.Background(), symbol)
	return args.Get(0).([]*pb.Position), args.Error(1)
}
func (m *MockExchange) GetBalance(ctx context.Context, asset string) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
func (m *MockExchange) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
	return nil
}
func (m *MockExchange) StopOrderStream() error { return nil }
func (m *MockExchange) StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error {
	return nil
}
func (m *MockExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error {
	return nil
}
func (m *MockExchange) StopKlineStream() error { return nil }
func (m *MockExchange) StartAccountStream(ctx context.Context, callback func(account *pb.Account)) error {
	return nil
}
func (m *MockExchange) StartPositionStream(ctx context.Context, callback func(position *pb.Position)) error {
	return nil
}
func (m *MockExchange) GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
func (m *MockExchange) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error) {
	args := m.Called(context.Background(), symbol, interval, limit)
	return args.Get(0).([]*pb.Candle), args.Error(1)
}
func (m *MockExchange) FetchExchangeInfo(ctx context.Context, symbol string) error { return nil }
func (m *MockExchange) GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error) {
	return nil, nil
}
func (m *MockExchange) GetPriceDecimals() int    { return 8 }
func (m *MockExchange) GetQuantityDecimals() int { return 8 }
func (m *MockExchange) GetBaseAsset() string     { return "BTC" }
func (m *MockExchange) GetQuoteAsset() string    { return "USDT" }
func (m *MockExchange) GetFundingRate(ctx context.Context, symbol string) (*pb.FundingRate, error) {
	return nil, nil
}
func (m *MockExchange) GetFundingRates(ctx context.Context) ([]*pb.FundingRate, error) {
	return nil, nil
}
func (m *MockExchange) GetHistoricalFundingRates(ctx context.Context, symbol string, limit int) ([]*pb.FundingRate, error) {
	return nil, nil
}
func (m *MockExchange) GetTickers(ctx context.Context) ([]*pb.Ticker, error) {
	return nil, nil
}
func (m *MockExchange) GetOpenInterest(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
func (m *MockExchange) StartFundingRateStream(ctx context.Context, symbol string, callback func(update *pb.FundingUpdate)) error {
	return nil
}
func (m *MockExchange) GetSymbols(ctx context.Context) ([]string, error) {
	return []string{"BTCUSDT"}, nil
}
