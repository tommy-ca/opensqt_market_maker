package grid

import (
	"context"
	"market_maker/internal/pb"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestGridStrategy_TrendFollowing(t *testing.T) {
	// Base Interval = 10.0
	cfg := StrategyConfig{
		Symbol:              "BTCUSDT",
		Exchange:            "mock",
		PriceInterval:       decimal.NewFromFloat(10.0),
		OrderQuantity:       decimal.NewFromFloat(1.0),
		MinOrderValue:       decimal.NewFromFloat(5.0),
		BuyWindowSize:       2,
		SellWindowSize:      2,
		PriceDecimals:       2,
		QtyDecimals:         3,
		IsNeutral:           false,
		InventorySkewFactor: 0.001, // 0.1% per unit deviation
	}
	strat := NewGridStrategy(cfg)

	anchor := decimal.NewFromFloat(50000.0)
	current := decimal.NewFromFloat(50000.0)

	// Case 1: Excessive Inventory (Long) -> Expect price skew DOWN (to sell easier/buy lower)
	// Add 10 units of inventory.
	levels := []GridLevel{}
	for i := 0; i < 10; i++ {
		price := decimal.NewFromFloat(49000.0 + float64(i))
		levels = append(levels, GridLevel{
			Price:          price,
			PositionStatus: pb.PositionStatus_POSITION_STATUS_FILLED,
			PositionQty:    decimal.NewFromFloat(1.0),
		})
	}

	target, err := strat.CalculateTargetState(context.Background(), current, anchor, decimal.Zero, 0, false, false, levels)
	assert.NoError(t, err)

	// With +10 inventory and skew 0.001:
	// Adjustment = 1 - (10 * 0.001) = 0.99
	// Skewed Price = 50000 * 0.99 = 49500
	// Grid is centered at 49500.
	// Buy Window: [49490, 49480]

	foundBuyNear50k := false
	foundBuyNear49500 := false

	for _, o := range target.Orders {
		if o.Side == "BUY" {
			if o.Price.GreaterThan(decimal.NewFromFloat(49900.0)) {
				foundBuyNear50k = true
			}
			if o.Price.LessThan(decimal.NewFromFloat(49600.0)) {
				foundBuyNear49500 = true
			}
		}
	}

	assert.False(t, foundBuyNear50k, "Should not buy near 50k when skewed")
	assert.True(t, foundBuyNear49500, "Should buy near 49.5k when skewed")
}
