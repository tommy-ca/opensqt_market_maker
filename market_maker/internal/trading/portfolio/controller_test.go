package portfolio

import (
	"context"
	"encoding/json"
	"market_maker/internal/core"
	"market_maker/internal/engine"
	"market_maker/internal/pb"
	"market_maker/internal/trading/arbitrage"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockOrchestrator struct {
	mock.Mock
}

func (m *mockOrchestrator) AddSymbol(symbol string, eng engine.Engine) {
	m.Called(symbol, eng)
}

func (m *mockOrchestrator) RemoveSymbol(symbol string) {
	m.Called(symbol)
}

func (m *mockOrchestrator) StartSymbol(symbol string) error {
	args := m.Called(symbol)
	return args.Error(0)
}

func (m *mockOrchestrator) GetEngine(symbol string) (engine.Engine, bool) {
	args := m.Called(symbol)
	if args.Get(0) == nil {
		return nil, false
	}
	return args.Get(0).(engine.Engine), true
}

func (m *mockOrchestrator) GetSymbols() []string {
	args := m.Called()
	return args.Get(0).([]string)
}

func (m *mockOrchestrator) AddTradingPair(ctx context.Context, symbol string, exchange string, config json.RawMessage, targetNotional decimal.Decimal, qualityScore decimal.Decimal, sector string) error {
	args := m.Called(ctx, symbol, exchange, config, targetNotional, qualityScore, sector)
	return args.Error(0)
}

func (m *mockOrchestrator) RemoveTradingPair(ctx context.Context, symbol string) error {
	args := m.Called(ctx, symbol)
	return args.Error(0)
}

type mockPortfolioEngine struct {
	mock.Mock
	engine.Engine
}

func (m *mockPortfolioEngine) SetOrderQuantity(qty decimal.Decimal) {
	m.Called(qty)
}

func (m *mockPortfolioEngine) GetOrderQuantity() decimal.Decimal {
	args := m.Called()
	return args.Get(0).(decimal.Decimal)
}

func (m *mockPortfolioEngine) Start(ctx context.Context) error { return nil }

type mockScanner struct {
	mock.Mock
}

func (m *mockScanner) Scan(ctx context.Context) ([]arbitrage.Opportunity, error) {
	args := m.Called(ctx)
	return args.Get(0).([]arbitrage.Opportunity), args.Error(1)
}

func (m *mockScanner) CreateEngine(symbol string, config json.RawMessage) (engine.Engine, error) {
	args := m.Called(symbol, config)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(engine.Engine), args.Error(1)
}

func (m *mockScanner) CreateConfig(symbol string, notional decimal.Decimal) (json.RawMessage, error) {
	args := m.Called(symbol, notional)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func TestPortfolioController_Rebalance(t *testing.T) {
	// Setup
	selector := new(mockScanner)
	allocator := NewPortfolioAllocator()
	marginSim := NewMarginSim()
	orch := new(mockOrchestrator)
	logger := new(mockLogger)

	controller := NewPortfolioController(selector, allocator, marginSim, orch, logger, time.Hour)

	// Mock Opportunity
	opps := []arbitrage.Opportunity{
		{Symbol: "BTCUSDT", QualityScore: decimal.NewFromInt(1), Metrics: arbitrage.FundingMetrics{AverageAnnualAPR: decimal.NewFromFloat(0.20)}},
	}
	selector.On("Scan", mock.Anything).Return(opps, nil)

	// Mock Account
	marginSim.UpdateAccount(&pb.Account{
		IsUnified:              true,
		AdjustedEquity:         pbu.FromGoDecimal(decimal.NewFromInt(10000)),
		TotalMaintenanceMargin: pbu.FromGoDecimal(decimal.NewFromInt(1000)),
		HealthScore:            pbu.FromGoDecimal(decimal.NewFromFloat(0.8)),
	})

	// 1. Initial Addition
	mockEng := new(mockPortfolioEngine)
	selector.On("CreateConfig", "BTCUSDT", mock.Anything).Return(json.RawMessage("{}"), nil)
	selector.On("CreateEngine", "BTCUSDT", mock.Anything).Return(mockEng, nil)
	orch.On("AddTradingPair", mock.Anything, "BTCUSDT", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	orch.On("AddSymbol", "BTCUSDT", mockEng).Return()
	orch.On("StartSymbol", "BTCUSDT").Return(nil)

	err := controller.Rebalance(context.Background())
	assert.NoError(t, err)

	selector.AssertExpectations(t)
	orch.AssertExpectations(t)

	// 2. Rebalance (Resize)
	mockEng.On("GetOrderQuantity").Return(decimal.NewFromInt(1000))
	selector.On("CreateConfig", "BTCUSDT", mock.Anything).Return(json.RawMessage("{}"), nil)
	orch.On("AddTradingPair", mock.Anything, "BTCUSDT", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	// Target = 10000 * 3 * 0.25 = 7500
	mockEng.On("SetOrderQuantity", mock.MatchedBy(func(d decimal.Decimal) bool {
		return d.Equal(decimal.NewFromInt(7500))
	})).Return()

	err = controller.Rebalance(context.Background())
	assert.NoError(t, err)
	mockEng.AssertExpectations(t)
}

func TestPortfolioRecovery(t *testing.T) {
	// Manual test for recovery logic
}

type mockLogger struct {
	core.ILogger
}

func (m *mockLogger) Info(msg string, f ...interface{})              {}
func (m *mockLogger) Error(msg string, f ...interface{})             {}
func (m *mockLogger) Warn(msg string, f ...interface{})              {}
func (m *mockLogger) Debug(msg string, f ...interface{})             {}
func (m *mockLogger) WithField(k string, v interface{}) core.ILogger { return m }
