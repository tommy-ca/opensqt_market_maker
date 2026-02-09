package grid

import (
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestGridStrategy_CalculateActions_Neutral(t *testing.T) {
	cfg := StrategyConfig{
		Symbol:         "BTCUSDT",
		PriceInterval:  decimal.NewFromFloat(10.0),
		OrderQuantity:  decimal.NewFromFloat(100.0),
		MinOrderValue:  decimal.NewFromFloat(5.0),
		BuyWindowSize:  2,
		SellWindowSize: 2,
		PriceDecimals:  2,
		QtyDecimals:    3,
		IsNeutral:      true,
	}
	strat := NewStrategy(cfg)

	anchorPrice := decimal.NewFromFloat(45000.0)
	currentPrice := decimal.NewFromFloat(45000.0)
	levels := []Slot{}

	// Test 1: Initial state (should have 2 Buy and 2 Sell orders)
	actions := strat.CalculateActions(currentPrice, anchorPrice, decimal.Zero, 0, false, pb.MarketRegime_MARKET_REGIME_RANGE, levels)

	buyCount := 0
	sellCount := 0
	for _, o := range actions {
		if o.Request.Side == pb.OrderSide_ORDER_SIDE_BUY {
			buyCount++
		} else if o.Request.Side == pb.OrderSide_ORDER_SIDE_SELL {
			sellCount++
		}
	}

	if buyCount != 2 || sellCount != 2 {
		assert.Equal(t, 2, buyCount, "Expected 2 Buy orders")
		assert.Equal(t, 2, sellCount, "Expected 2 Sell orders")
	}
}

func TestGridStrategy_WithRiskTriggered(t *testing.T) {
	cfg := StrategyConfig{
		Symbol:         "BTCUSDT",
		PriceInterval:  decimal.NewFromFloat(10.0),
		OrderQuantity:  decimal.NewFromFloat(100.0),
		MinOrderValue:  decimal.NewFromFloat(5.0),
		BuyWindowSize:  2,
		SellWindowSize: 2,
		PriceDecimals:  2,
		QtyDecimals:    3,
		IsNeutral:      true,
	}
	strat := NewStrategy(cfg)

	// When risk is triggered, we want no opening BUY orders. Sells are allowed (e.g. to reduce inventory or short).
	actions := strat.CalculateActions(decimal.NewFromFloat(45000.0), decimal.NewFromFloat(45000.0), decimal.Zero, 0, true, pb.MarketRegime_MARKET_REGIME_RANGE, []Slot{})

	// Should have Sells but NO Buys
	for _, a := range actions {
		if a.Request.Side == pb.OrderSide_ORDER_SIDE_BUY {
			assert.Fail(t, "Expected no BUY orders when risk is triggered, got BUY")
		}
	}

	// But if we have an existing position, we should still have a closing order
	levels := []Slot{
		{
			Price:          decimal.NewFromFloat(44990.0),
			PositionStatus: pb.PositionStatus_POSITION_STATUS_FILLED,
			PositionQty:    decimal.NewFromFloat(100.0),
			SlotStatus:     pb.SlotStatus_SLOT_STATUS_FREE,
		},
	}
	actions = strat.CalculateActions(decimal.NewFromFloat(45000.0), decimal.NewFromFloat(45000.0), decimal.Zero, 0, true, pb.MarketRegime_MARKET_REGIME_RANGE, levels)

	// Expect closing sell order + potentially opening sell orders
	foundClosing := false
	for _, a := range actions {
		if a.Request.Side == pb.OrderSide_ORDER_SIDE_BUY {
			assert.Fail(t, "Expected no BUY orders when risk is triggered, got BUY")
		}
		// Check for closing order (logic: if Price > Filled Price, likely closing for Long)
		// Filled at 44990. Interval 10. Sell at 45000?
		// Neutral closing logic:
		// if PositionQty > 0 (100.0): close at Price + Interval = 44990 + 10 = 45000.
		if a.Request.Side == pb.OrderSide_ORDER_SIDE_SELL {
			price := pbu.ToGoDecimal(a.Request.Price)
			if price.Equal(decimal.NewFromFloat(45000.0)) {
				foundClosing = true
			}
		}
	}

	assert.True(t, foundClosing, "Expected closing SELL order at 45000.0")
}
