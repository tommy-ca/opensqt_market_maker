package orchestrator

import (
	"context"
	"encoding/json"
	"market_maker/internal/core"
	"market_maker/internal/engine"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
)

type mockFactory struct{}

func (f *mockFactory) CreateEngine(symbol string, config json.RawMessage) (engine.Engine, error) {
	return &mockEngine{}, nil
}

type mockEngine struct {
	mock.Mock
}

func (m *mockEngine) Start(ctx context.Context) error {
	return nil
}

func (m *mockEngine) Stop() error {
	return nil
}

func (m *mockEngine) OnPriceUpdate(ctx context.Context, price *pb.PriceChange) error {
	args := m.Called(ctx, price)
	return args.Error(0)
}

func (m *mockEngine) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	args := m.Called(ctx, update)
	return args.Error(0)
}

func (m *mockEngine) OnFundingUpdate(ctx context.Context, update *pb.FundingUpdate) error {
	args := m.Called(ctx, update)
	return args.Error(0)
}

func (m *mockEngine) OnPositionUpdate(ctx context.Context, position *pb.Position) error {
	args := m.Called(ctx, position)
	return args.Error(0)
}

func (m *mockEngine) OnAccountUpdate(ctx context.Context, account *pb.Account) error {
	args := m.Called(ctx, account)
	return args.Error(0)
}

type mockExchange struct {
	core.IExchange
	mock.Mock
}

func (m *mockExchange) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
	m.Called(ctx, callback)
	return nil
}

func (m *mockExchange) StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error {
	m.Called(ctx, symbols, callback)
	return nil
}

func (m *mockExchange) GetName() string {
	return "mock"
}

type mockLogger struct {
	core.ILogger
}

func (m *mockLogger) Info(msg string, args ...interface{})                 {}
func (m *mockLogger) Error(msg string, args ...interface{})                {}
func (m *mockLogger) Warn(msg string, args ...interface{})                 {}
func (m *mockLogger) Debug(msg string, args ...interface{})                {}
func (m *mockLogger) WithField(key string, value interface{}) core.ILogger { return m }

func TestOrchestrator_Routing(t *testing.T) {
	exch := &mockExchange{}
	logger := &mockLogger{}
	factory := &mockFactory{}
	orch := NewOrchestrator(exch, factory, logger)

	engineBTC := &mockEngine{}
	engineETH := &mockEngine{}

	orch.AddSymbol("BTCUSDT", engineBTC)
	orch.AddSymbol("ETHUSDT", engineETH)

	// Expect routing to correct engines
	priceBTC := &pb.PriceChange{Symbol: "BTCUSDT", Price: pbu.FromGoDecimal(decimal.NewFromInt(45000))}
	priceETH := &pb.PriceChange{Symbol: "ETHUSDT", Price: pbu.FromGoDecimal(decimal.NewFromInt(2500))}

	engineBTC.On("OnPriceUpdate", mock.Anything, priceBTC).Return(nil).Once()
	engineETH.On("OnPriceUpdate", mock.Anything, priceETH).Return(nil).Once()

	// Start managers (manual start for test to avoid stream complexity)
	for _, m := range orch.managers {
		_ = m.Start()
		defer m.Stop()
	}

	orch.routePriceUpdate(priceBTC)
	orch.routePriceUpdate(priceETH)

	// Wait for processing (it's async via channels)
	time.Sleep(100 * time.Millisecond)

	engineBTC.AssertExpectations(t)
	engineETH.AssertExpectations(t)
}

func TestOrchestrator_OrderRouting(t *testing.T) {
	exch := &mockExchange{}
	logger := &mockLogger{}
	factory := &mockFactory{}
	orch := NewOrchestrator(exch, factory, logger)

	engineBTC := &mockEngine{}
	orch.AddSymbol("BTCUSDT", engineBTC)

	update := &pb.OrderUpdate{Symbol: "BTCUSDT", OrderId: 123, Status: pb.OrderStatus_ORDER_STATUS_FILLED}
	engineBTC.On("OnOrderUpdate", mock.Anything, update).Return(nil).Once()

	for _, m := range orch.managers {
		_ = m.Start()
		defer m.Stop()
	}

	orch.routeOrderUpdate(update)

	time.Sleep(100 * time.Millisecond)
	engineBTC.AssertExpectations(t)
}
