package arbengine_test

import (
	"context"
	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/internal/engine/arbengine"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/internal/trading/monitor"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// Reuse MockLogger from other tests or define locally
type MockLogger struct{}

func (m *MockLogger) Debug(msg string, fields ...interface{})               {}
func (m *MockLogger) Info(msg string, fields ...interface{})                {}
func (m *MockLogger) Warn(msg string, fields ...interface{})                {}
func (m *MockLogger) Error(msg string, fields ...interface{})               {}
func (m *MockLogger) Fatal(msg string, fields ...interface{})               {}
func (m *MockLogger) WithField(key string, value interface{}) core.ILogger  { return m }
func (m *MockLogger) WithFields(fields map[string]interface{}) core.ILogger { return m }

func TestArbitrageEngine_Entry(t *testing.T) {
	spotEx := mock.NewMockExchange("binance_spot")
	perpEx := mock.NewMockExchange("binance")

	exchanges := map[string]core.IExchange{
		"binance_spot": spotEx,
		"binance":      perpEx,
	}

	ctx := context.Background()
	logger := &MockLogger{}
	cfg := config.DefaultConfig()
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.ArbitrageSpotExchange = "binance_spot"
	cfg.Trading.ArbitragePerpExchange = "binance"
	cfg.Trading.MinSpreadAPR = 0.10

	fundingMonitor := monitor.NewFundingMonitor(exchanges, logger, cfg.Trading.Symbol)
	fundingMonitor.Start(ctx)

	eng := arbengine.NewArbitrageEngine(exchanges, nil, fundingMonitor, logger, arbengine.EngineConfig{
		Symbol:                    cfg.Trading.Symbol,
		SpotExchange:              cfg.Trading.ArbitrageSpotExchange,
		PerpExchange:              cfg.Trading.ArbitragePerpExchange,
		MinSpreadAPR:              decimal.NewFromFloat(cfg.Trading.MinSpreadAPR),
		ExitSpreadAPR:             decimal.NewFromFloat(cfg.Trading.ExitSpreadAPR),
		LiquidationThreshold:      decimal.NewFromFloat(cfg.Trading.LiquidationThreshold),
		OrderQuantity:             decimal.NewFromFloat(cfg.Trading.OrderQuantity),
		FundingStalenessThreshold: time.Minute,
	})
	eng.Start(ctx)

	// Simulate High Funding Rate Update for both legs
	// Spot: 0%
	// Perp: 0.05% -> 54% APR
	spotEx.SetFundingRate("BTCUSDT", decimal.Zero)
	perpEx.SetFundingRate("BTCUSDT", decimal.NewFromFloat(0.0005))

	// Re-fetch to ensure monitor has both
	fundingMonitor.Start(ctx)

	update := &pb.FundingUpdate{
		Exchange:  "binance",
		Symbol:    "BTCUSDT",
		Rate:      pbu.FromGoDecimal(decimal.NewFromFloat(0.0005)),
		Timestamp: time.Now().UnixMilli(),
	}

	err := eng.OnFundingUpdate(ctx, update)
	assert.NoError(t, err)

	// Verify Orders
	// Spot should have Buy
	spotOrders := spotEx.GetOrders()
	assert.Len(t, spotOrders, 1)
	assert.Equal(t, pb.OrderSide_ORDER_SIDE_BUY, spotOrders[0].Side)

	// Perp should have Sell
	perpOrders := perpEx.GetOrders()
	assert.Len(t, perpOrders, 1)
	assert.Equal(t, pb.OrderSide_ORDER_SIDE_SELL, perpOrders[0].Side)
}

func TestArbitrageEngine_StalenessGating(t *testing.T) {
	spotEx := mock.NewMockExchange("binance_spot")
	perpEx := mock.NewMockExchange("binance")
	exchanges := map[string]core.IExchange{"binance_spot": spotEx, "binance": perpEx}
	logger := &MockLogger{}
	ctx := context.Background()

	fundingMonitor := monitor.NewFundingMonitor(exchanges, logger, "BTCUSDT")
	fundingMonitor.Start(ctx)

	eng := arbengine.NewArbitrageEngine(exchanges, nil, fundingMonitor, logger, arbengine.EngineConfig{
		Symbol:                    "BTCUSDT",
		SpotExchange:              "binance_spot",
		PerpExchange:              "binance",
		MinSpreadAPR:              decimal.NewFromFloat(0.10),
		OrderQuantity:             decimal.NewFromFloat(1.0),
		FundingStalenessThreshold: 100 * time.Millisecond,
	})
	eng.Start(ctx)

	// Set fresh rates
	spotEx.SetFundingRate("BTCUSDT", decimal.Zero)
	perpEx.SetFundingRate("BTCUSDT", decimal.NewFromFloat(0.001))
	fundingMonitor.Start(ctx) // Sync

	// Wait for staleness
	time.Sleep(200 * time.Millisecond)

	update := &pb.FundingUpdate{Exchange: "binance", Symbol: "BTCUSDT", Rate: pbu.FromGoDecimal(decimal.NewFromFloat(0.001))}
	err := eng.OnFundingUpdate(ctx, update)
	assert.NoError(t, err)

	// No orders should be placed because feed is stale
	assert.Len(t, spotEx.GetOrders(), 0)
	assert.Len(t, perpEx.GetOrders(), 0)
}

func TestArbitrageEngine_NegativeFunding(t *testing.T) {
	spotEx := mock.NewMockExchange("binance_spot")
	perpEx := mock.NewMockExchange("binance")
	exchanges := map[string]core.IExchange{"binance_spot": spotEx, "binance": perpEx}
	logger := &MockLogger{}
	ctx := context.Background()

	fundingMonitor := monitor.NewFundingMonitor(exchanges, logger, "BTCUSDT")
	fundingMonitor.Start(ctx)

	eng := arbengine.NewArbitrageEngine(exchanges, nil, fundingMonitor, logger, arbengine.EngineConfig{
		Symbol:                    "BTCUSDT",
		SpotExchange:              "binance_spot",
		PerpExchange:              "binance",
		MinSpreadAPR:              decimal.NewFromFloat(0.10),
		OrderQuantity:             decimal.NewFromFloat(1.0),
		FundingStalenessThreshold: time.Minute,
	})
	eng.Start(ctx)

	// Simulate Negative Funding (Perp receives)
	// Spot: 0%
	// Perp: -0.05% -> -54% APR (Spread = -0.05% - 0% = -0.05%)
	spotEx.SetFundingRate("BTCUSDT", decimal.Zero)
	perpEx.SetFundingRate("BTCUSDT", decimal.NewFromFloat(-0.0005))
	fundingMonitor.Start(ctx)

	update := &pb.FundingUpdate{
		Exchange:  "binance",
		Symbol:    "BTCUSDT",
		Rate:      pbu.FromGoDecimal(decimal.NewFromFloat(-0.0005)),
		Timestamp: time.Now().UnixMilli(),
	}

	err := eng.OnFundingUpdate(ctx, update)
	assert.NoError(t, err)

	// Verify Orders (Reverse Direction)
	// Spot should have Sell
	spotOrders := spotEx.GetOrders()
	assert.Len(t, spotOrders, 1)
	assert.Equal(t, pb.OrderSide_ORDER_SIDE_SELL, spotOrders[0].Side)

	// Perp should have Buy
	perpOrders := perpEx.GetOrders()
	assert.Len(t, perpOrders, 1)
	assert.Equal(t, pb.OrderSide_ORDER_SIDE_BUY, perpOrders[0].Side)
}
