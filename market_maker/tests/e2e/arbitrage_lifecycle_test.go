package e2e

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

type mockArbLogger struct{ core.ILogger }

func (m *mockArbLogger) Info(msg string, args ...interface{})             {}
func (m *mockArbLogger) Warn(msg string, args ...interface{})             {}
func (m *mockArbLogger) Error(msg string, args ...interface{})            {}
func (m *mockArbLogger) WithField(k string, v interface{}) core.ILogger   { return m }
func (m *mockArbLogger) WithFields(f map[string]interface{}) core.ILogger { return m }

func TestE2E_ArbitrageLifecycle(t *testing.T) {
	// 1. Setup
	spotEx := mock.NewMockExchange("binance_spot")
	perpEx := mock.NewMockExchange("binance")
	exchanges := map[string]core.IExchange{
		"binance_spot": spotEx,
		"binance":      perpEx,
	}

	cfg := config.DefaultConfig()
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.ArbitrageSpotExchange = "binance_spot"
	cfg.Trading.ArbitragePerpExchange = "binance"
	cfg.Trading.MinSpreadAPR = 0.10         // 10%
	cfg.Trading.ExitSpreadAPR = 0.01        // 1%
	cfg.Trading.LiquidationThreshold = 0.10 // 10%
	cfg.Trading.OrderQuantity = 1.0

	fundingMonitor := monitor.NewFundingMonitor(exchanges, &mockArbLogger{}, cfg.Trading.Symbol)
	_ = fundingMonitor.Start(context.Background())

	eng := arbengine.NewArbitrageEngine(exchanges, nil, fundingMonitor, &mockArbLogger{}, arbengine.EngineConfig{
		Symbol:                    cfg.Trading.Symbol,
		SpotExchange:              cfg.Trading.ArbitrageSpotExchange,
		PerpExchange:              cfg.Trading.ArbitragePerpExchange,
		OrderQuantity:             decimal.NewFromFloat(cfg.Trading.OrderQuantity),
		MinSpreadAPR:              decimal.NewFromFloat(cfg.Trading.MinSpreadAPR),
		ExitSpreadAPR:             decimal.NewFromFloat(cfg.Trading.ExitSpreadAPR),
		LiquidationThreshold:      decimal.NewFromFloat(cfg.Trading.LiquidationThreshold),
		FundingStalenessThreshold: time.Minute,
	})

	ctx := context.Background()
	_ = eng.Start(ctx)

	// SCENARIO 1: ENTRY
	// Spot: 0, Perp: 0.05% -> 54% APR (> 10%)
	spotEx.SetFundingRate("BTCUSDT", decimal.Zero)
	perpEx.SetFundingRate("BTCUSDT", decimal.NewFromFloat(0.0005))
	_ = fundingMonitor.Start(ctx)

	updateEntry := &pb.FundingUpdate{
		Exchange:        "binance",
		Symbol:          "BTCUSDT",
		Rate:            pbu.FromGoDecimal(decimal.NewFromFloat(0.0005)),
		NextFundingTime: time.Now().Add(1 * time.Hour).UnixMilli(),
	}
	_ = eng.OnFundingUpdate(ctx, updateEntry)

	assert.Len(t, spotEx.GetOrders(), 1, "Should have 1 spot order")
	assert.Len(t, perpEx.GetOrders(), 1, "Should have 1 perp order")

	// Simulate fills
	spotEx.SetPosition("BTCUSDT", decimal.NewFromInt(1))
	perpEx.SetPosition("BTCUSDT", decimal.NewFromInt(-1))

	// Force a sync
	_ = eng.OnOrderUpdate(ctx, &pb.OrderUpdate{Exchange: "binance_spot", Symbol: "BTCUSDT", Status: pb.OrderStatus_ORDER_STATUS_FILLED})
	_ = eng.OnOrderUpdate(ctx, &pb.OrderUpdate{Exchange: "binance", Symbol: "BTCUSDT", Status: pb.OrderStatus_ORDER_STATUS_FILLED})

	// SCENARIO 2: EXIT (CONVERGENCE)
	// 0.00001% -> ~0% APR (< 1%)
	spotEx.SetFundingRate("BTCUSDT", decimal.Zero)
	perpEx.SetFundingRate("BTCUSDT", decimal.NewFromFloat(0.0000001))
	_ = fundingMonitor.Start(ctx)

	updateExit := &pb.FundingUpdate{
		Exchange:        "binance",
		Symbol:          "BTCUSDT",
		Rate:            pbu.FromGoDecimal(decimal.NewFromFloat(0.0000001)),
		NextFundingTime: time.Now().Add(2 * time.Hour).UnixMilli(), // Different interval to allow action
	}
	_ = eng.OnFundingUpdate(ctx, updateExit)

	assert.Len(t, spotEx.GetOrders(), 2, "Should have 2 spot orders total")
	assert.Len(t, perpEx.GetOrders(), 2, "Should have 2 perp orders total")

	// SCENARIO 3: EMERGENCY EXIT (LIQUIDATION)
	// Reset positions for new scenario
	spotEx.SetPosition("BTCUSDT", decimal.NewFromInt(1))
	perpEx.SetPosition("BTCUSDT", decimal.NewFromInt(-1))
	_ = eng.OnOrderUpdate(ctx, &pb.OrderUpdate{Exchange: "binance", Symbol: "BTCUSDT", Status: pb.OrderStatus_ORDER_STATUS_FILLED})

	// Price is 100, Liq is 105 (Short) -> Distance 5% (< 10% threshold)
	liqPos := &pb.Position{
		Symbol:           "BTCUSDT",
		Size:             pbu.FromGoDecimal(decimal.NewFromInt(-1)),
		MarkPrice:        pbu.FromGoDecimal(decimal.NewFromInt(100)),
		LiquidationPrice: pbu.FromGoDecimal(decimal.NewFromInt(105)),
	}
	_ = eng.OnPositionUpdate(ctx, liqPos)

	// Expect total 3 orders per exchange
	assert.Len(t, spotEx.GetOrders(), 3)
	assert.Len(t, perpEx.GetOrders(), 3)
}
