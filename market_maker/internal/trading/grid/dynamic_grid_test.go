package grid

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestGridStrategy_DynamicInterval(t *testing.T) {
	cfg := StrategyConfig{
		Symbol:          "BTCUSDT",
		Exchange:        "mock",
		PriceInterval:   decimal.NewFromFloat(10.0),
		OrderQuantity:   decimal.NewFromFloat(1.0),
		MinOrderValue:   decimal.NewFromFloat(5.0),
		BuyWindowSize:   2,
		SellWindowSize:  2,
		PriceDecimals:   2,
		QtyDecimals:     3,
		IsNeutral:       false,
		VolatilityScale: 1.0,
	}
	strat := NewGridStrategy(cfg)

	anchor := decimal.NewFromFloat(50000.0)
	current := decimal.NewFromFloat(49996.0)

	// Case 1: Low Volatility (ATR = 5.0) -> Should use Base Interval (10.0)
	// Because Max(10, 5*1) = 10
	target, err := strat.CalculateTargetState(context.Background(), current, anchor, decimal.NewFromFloat(5.0), 0, false, false, []GridLevel{})
	assert.NoError(t, err)

	// Expect Buy order at Anchor - Interval = 50000 - 10 = 49990
	found49990 := false
	for _, o := range target.Orders {
		if o.Price.Equal(decimal.NewFromFloat(49990.0)) {
			found49990 = true
		}
	}
	assert.True(t, found49990, "Should find buy order at 49990 with base interval")

	// Case 2: High Volatility (ATR = 50.0) -> Should use ATR based Interval (50.0)
	// Max(10, 50*1) = 50
	target, err = strat.CalculateTargetState(context.Background(), current, anchor, decimal.NewFromFloat(50.0), 0, false, false, []GridLevel{})
	assert.NoError(t, err)

	// Expect Buy order at Anchor - 50 = 49950
	found49950 := false
	found49990 = false
	for _, o := range target.Orders {
		if o.Price.Equal(decimal.NewFromFloat(49950.0)) {
			found49950 = true
		}
		if o.Price.Equal(decimal.NewFromFloat(49990.0)) {
			found49990 = true
		}
	}
	assert.True(t, found49950, "Should find buy order at 49950 with dynamic interval")
	assert.False(t, found49990, "Should NOT find buy order at 49990 (old interval)")
}
