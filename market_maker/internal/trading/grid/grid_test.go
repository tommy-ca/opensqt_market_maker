package grid

import (
	"context"
	"market_maker/internal/pb"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestGridStrategy_CalculateTargetState_Neutral(t *testing.T) {
	cfg := StrategyConfig{
		Symbol:         "BTCUSDT",
		Exchange:       "mock",
		PriceInterval:  decimal.NewFromFloat(10.0),
		OrderQuantity:  decimal.NewFromFloat(100.0),
		MinOrderValue:  decimal.NewFromFloat(5.0),
		BuyWindowSize:  2,
		SellWindowSize: 2,
		PriceDecimals:  2,
		QtyDecimals:    3,
		IsNeutral:      true,
	}
	strat := NewGridStrategy(cfg)

	anchorPrice := decimal.NewFromFloat(45000.0)
	currentPrice := decimal.NewFromFloat(45000.0)
	levels := []GridLevel{}

	// Test 1: Initial state (should have 2 Buy and 2 Sell orders)
	target, err := strat.CalculateTargetState(context.Background(), currentPrice, anchorPrice, decimal.Zero, 0, false, false, levels)
	assert.NoError(t, err)

	buyCount := 0
	sellCount := 0
	for _, o := range target.Orders {
		if o.Side == "BUY" {
			buyCount++
		} else if o.Side == "SELL" {
			sellCount++
		}
	}

	if buyCount != 2 || sellCount != 2 {
		t.Errorf("Expected 2 Buy and 2 Sell orders, got %d Buy and %d Sell", buyCount, sellCount)
	}
}

func TestGridStrategy_WithCircuitBreaker(t *testing.T) {
	cfg := StrategyConfig{
		Symbol:         "BTCUSDT",
		Exchange:       "mock",
		PriceInterval:  decimal.NewFromFloat(10.0),
		OrderQuantity:  decimal.NewFromFloat(100.0),
		MinOrderValue:  decimal.NewFromFloat(5.0),
		BuyWindowSize:  2,
		SellWindowSize: 2,
		PriceDecimals:  2,
		QtyDecimals:    3,
		IsNeutral:      true,
	}
	strat := NewGridStrategy(cfg)

	target, err := strat.CalculateTargetState(context.Background(), decimal.NewFromFloat(45000.0), decimal.NewFromFloat(45000.0), decimal.Zero, 0, false, true, []GridLevel{})
	assert.NoError(t, err)

	if len(target.Orders) != 0 {
		t.Errorf("Expected 0 orders when circuit breaker is tripped, got %d", len(target.Orders))
	}
}

func TestGridStrategy_WithRiskTriggered(t *testing.T) {
	cfg := StrategyConfig{
		Symbol:         "BTCUSDT",
		Exchange:       "mock",
		PriceInterval:  decimal.NewFromFloat(10.0),
		OrderQuantity:  decimal.NewFromFloat(100.0),
		MinOrderValue:  decimal.NewFromFloat(5.0),
		BuyWindowSize:  2,
		SellWindowSize: 2,
		PriceDecimals:  2,
		QtyDecimals:    3,
		IsNeutral:      true,
	}
	strat := NewGridStrategy(cfg)

	// When risk is triggered, we want no opening orders (Buy or Sell)
	target, err := strat.CalculateTargetState(context.Background(), decimal.NewFromFloat(45000.0), decimal.NewFromFloat(45000.0), decimal.Zero, 0, true, false, []GridLevel{})
	assert.NoError(t, err)

	if len(target.Orders) != 0 {
		t.Errorf("Expected 0 orders when risk is triggered, got %d", len(target.Orders))
	}

	// But if we have an existing position, we should still have a closing order
	levels := []GridLevel{
		{
			Price:          decimal.NewFromFloat(44990.0),
			PositionStatus: pb.PositionStatus_POSITION_STATUS_FILLED,
			PositionQty:    decimal.NewFromFloat(100.0),
		},
	}
	target, err = strat.CalculateTargetState(context.Background(), decimal.NewFromFloat(45000.0), decimal.NewFromFloat(45000.0), decimal.Zero, 0, true, false, levels)
	assert.NoError(t, err)

	if len(target.Orders) != 1 {
		t.Errorf("Expected 1 closing order even when risk is triggered, got %d", len(target.Orders))
	}
	if target.Orders[0].Side != "SELL" {
		t.Errorf("Expected closing SELL order, got %s", target.Orders[0].Side)
	}
}
