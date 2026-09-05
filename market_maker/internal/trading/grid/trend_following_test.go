package grid

import (
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestGridStrategy_TrendFollowing(t *testing.T) {
	// Base Interval = 10.0
	cfg := StrategyConfig{
		Symbol:              "BTCUSDT",
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
	strat := NewStrategy(cfg)

	anchor := decimal.NewFromFloat(50000.0)
	current := decimal.NewFromFloat(50000.0)

	// Case 1: Excessive Inventory (Long) -> Expect price skew DOWN (to sell easier/buy lower)
	// Add 10 units of inventory.
	levels := []core.StrategySlot{}
	for i := 0; i < 10; i++ {
		price := decimal.NewFromFloat(49000.0 + float64(i))
		levels = append(levels, core.StrategySlot{
			Price:          price,
			PositionStatus: pb.PositionStatus_POSITION_STATUS_FILLED,
			PositionQty:    decimal.NewFromFloat(1.0),
			SlotStatus:     pb.SlotStatus_SLOT_STATUS_FREE,
		})
	}

	actions := strat.CalculateActions(current, anchor, decimal.Zero, 0, false, pb.MarketRegime_MARKET_REGIME_RANGE, levels)

	// With +10 inventory and skew 0.001:
	// Adjustment = 1 - (10 * 0.001) = 0.99
	// Skewed Price = 50000 * 0.99 = 49500
	// Grid is centered at 49500.
	// Buy Window: [49490, 49480]

	foundBuyNear50k := false
	foundBuyNear49500 := false

	for _, o := range actions {
		if o.Request.Side == pb.OrderSide_ORDER_SIDE_BUY {
			price := pbu.ToGoDecimal(o.Request.Price)
			if price.GreaterThan(decimal.NewFromFloat(49900.0)) {
				foundBuyNear50k = true
			}
			if price.LessThan(decimal.NewFromFloat(49600.0)) {
				foundBuyNear49500 = true
			}
		}
	}

	assert.False(t, foundBuyNear50k, "Should not buy near 50k when skewed")
	assert.True(t, foundBuyNear49500, "Should buy near 49.5k when skewed")
}
