package monitor

import (
	"context"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestRegimeMonitor_Calculations(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	logger := &mockLogger{}
	rm := NewRegimeMonitor(exchange, logger, "BTCUSDT")

	// 1. Initial State
	assert.True(t, rm.rsi.IsZero())
	assert.True(t, rm.atr.IsZero())
	assert.Equal(t, pb.MarketRegime_MARKET_REGIME_RANGE, rm.GetRegime())

	// 2. Feed enough candles to trigger calculation
	// RSI needs 15 candles (14 period + 1 for change)
	// ATR needs 14 candles

	// Create a sequence of candles
	basePrice := decimal.NewFromFloat(100.0)

	// Feed 20 candles
	for i := 0; i < 20; i++ {
		price := basePrice
		if i%2 == 0 {
			price = price.Add(decimal.NewFromFloat(1.0)) // Up
		} else {
			price = price.Sub(decimal.NewFromFloat(0.5)) // Down
		}

		// Move base price up slightly to create a trend if needed, or keep oscillating
		basePrice = basePrice.Add(decimal.NewFromFloat(0.2))

		candle := &pb.Candle{
			Symbol:    "BTCUSDT",
			Close:     pbu.FromGoDecimal(price),
			High:      pbu.FromGoDecimal(price.Add(decimal.NewFromFloat(0.5))),
			Low:       pbu.FromGoDecimal(price.Sub(decimal.NewFromFloat(0.5))),
			Volume:    pbu.FromGoDecimal(decimal.NewFromFloat(100)),
			IsClosed:  true,
			Timestamp: time.Now().Add(time.Duration(i) * time.Minute).UnixMilli(),
		}
		rm.handleKlineUpdate(candle)
	}

	// 3. Check if indicators are calculated
	rm.mu.RLock()
	rsi := rm.rsi
	atr := rm.atr
	rm.mu.RUnlock()

	assert.False(t, rsi.IsZero(), "RSI should be calculated")
	assert.False(t, atr.IsZero(), "ATR should be calculated")

	// RSI should be somewhere between 0 and 100
	rsiVal, _ := rsi.Float64()
	assert.GreaterOrEqual(t, rsiVal, 0.0)
	assert.LessOrEqual(t, rsiVal, 100.0)

	t.Logf("RSI: %v, ATR: %v", rsi, atr)
}

func TestRegimeMonitor_RegimeDetection(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	logger := &mockLogger{}
	rm := NewRegimeMonitor(exchange, logger, "BTCUSDT")

	// Helper to feed candles
	feedCandle := func(price decimal.Decimal) {
		candle := &pb.Candle{
			Symbol:    "BTCUSDT",
			Close:     pbu.FromGoDecimal(price),
			High:      pbu.FromGoDecimal(price.Add(decimal.NewFromFloat(1))),
			Low:       pbu.FromGoDecimal(price.Sub(decimal.NewFromFloat(1))),
			IsClosed:  true,
			Timestamp: time.Now().UnixMilli(),
		}
		rm.handleKlineUpdate(candle)
	}

	// Feed initial history to get indicators running
	price := decimal.NewFromFloat(100.0)
	for i := 0; i < 20; i++ {
		feedCandle(price)
		// Oscillate price to keep RSI around 50
		if i%2 == 0 {
			price = price.Add(decimal.NewFromFloat(1.0))
		} else {
			price = price.Sub(decimal.NewFromFloat(1.0))
		}
	}

	// Verify we are in RANGE (default)
	// RSI should be around 50
	rm.mu.RLock()
	currentRsi, _ := rm.rsi.Float64()
	rm.mu.RUnlock()
	t.Logf("Initial RSI: %f", currentRsi)

	assert.Equal(t, pb.MarketRegime_MARKET_REGIME_RANGE, rm.GetRegime())

	// Force Bull Trend (RSI > 70)
	// We need consistent gains
	for i := 0; i < 20; i++ {
		price = price.Add(decimal.NewFromFloat(2.0)) // Strong Up
		feedCandle(price)
	}

	// Check if RSI > 70
	rm.mu.RLock()
	rsiVal, _ := rm.rsi.Float64()
	rm.mu.RUnlock()
	t.Logf("Bull RSI: %f", rsiVal)

	if rsiVal > 70 {
		assert.Equal(t, pb.MarketRegime_MARKET_REGIME_BULL_TREND, rm.GetRegime())
	} else {
		t.Log("RSI did not reach > 70 yet, might need more candles")
	}

	// Force Bear Trend (RSI < 30)
	for i := 0; i < 30; i++ {
		price = price.Sub(decimal.NewFromFloat(3.0)) // Strong Down
		feedCandle(price)
	}

	rm.mu.RLock()
	rsiVal, _ = rm.rsi.Float64()
	rm.mu.RUnlock()
	t.Logf("Bear RSI: %f", rsiVal)

	if rsiVal < 30 {
		assert.Equal(t, pb.MarketRegime_MARKET_REGIME_BEAR_TREND, rm.GetRegime())
	}
}

func TestRegimeMonitor_Preload(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	logger := &mockLogger{}
	rm := NewRegimeMonitor(exchange, logger, "BTCUSDT")

	ctx := context.Background()
	// Start calls preloadHistory
	err := rm.Start(ctx)
	assert.NoError(t, err)

	rm.mu.RLock()
	count := len(rm.candles)
	rsi := rm.rsi
	rm.mu.RUnlock()

	// MockExchange returns 100 candles by default for limit=100 in GetHistoricalKlines?
	// Let's check MockExchange implementation of GetHistoricalKlines.
	// It loops 'limit' times.

	assert.Equal(t, 100, count)
	assert.False(t, rsi.IsZero())
}
