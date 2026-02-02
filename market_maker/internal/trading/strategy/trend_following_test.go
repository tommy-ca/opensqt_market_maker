package strategy

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestGridStrategy_TrendFollowing(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)

	// Base Interval = 10.0
	strat := NewGridStrategy("BTCUSDT", "mock", decimal.NewFromFloat(10.0), decimal.NewFromFloat(1.0), decimal.NewFromFloat(5.0), 2, 2, 2, 3, false, nil, nil, logger)

	// Enable Trend Following with skew factor 0.001 (0.1% per unit deviation)
	strat.SetTrendFollowing(0.001)

	anchor := decimal.NewFromFloat(50000.0)
	current := decimal.NewFromFloat(50000.0)

	// Mock slots to simulate inventory
	slots := make(map[string]*core.InventorySlot)

	// Case 1: Excessive Inventory (Long) -> Expect price skew DOWN (to sell easier/buy lower)
	// Add 10 units of inventory. Target is 0 (neutral) or implied by slots?
	// The CalculateSkewedPrice logic compares inventory vs target.
	// For this test, let's assume we implement target as 0 deviation for now, or total position size?
	// The spec says "TargetInventory: Defined by user or neutral". Let's assume neutral (0) for relative skew.

	// Let's manually populate some filled buy slots to simulate inventory
	// 10 slots filled with 1.0 qty each = 10.0 inventory.
	for i := 0; i < 10; i++ {
		price := decimal.NewFromFloat(49000.0 + float64(i))
		slots[price.String()] = &core.InventorySlot{
			InventorySlot: &pb.InventorySlot{
				Price:          pbu.FromGoDecimal(price),
				PositionStatus: pb.PositionStatus_POSITION_STATUS_FILLED,
				PositionQty:    pbu.FromGoDecimal(decimal.NewFromFloat(1.0)),
			},
		}
	}

	actions, _ := strat.CalculateActions(context.Background(), slots, anchor, current)

	// With +10 inventory and skew 0.001:
	// Adjustment = 1 - (10 * 0.001) = 0.99
	// Effective Anchor = 50000 * 0.99 = 49500
	// Grid is centered at 49500.
	// Current price is 50000.
	// 50000 is 500 above anchor.
	// Base interval 10.
	// We expect grid levels relative to 49500.

	// With normal grid (50000), levels would be 49990, 49980...
	// With skewed grid (49500), levels around current (50000) would be ... 49990, 50000, 50010 ...
	// Wait, grid levels are calculated from anchor.
	// If anchor moves to 49500, then grid levels are 49500 +/- N*10.
	// 49500 + 50*10 = 50000.
	// So 50000 IS a grid level.

	// Let's verify that we are placing orders relative to the skewed anchor.
	// Or simply verify that the prices are lower than they would be without skew.

	// Without skew:
	// Anchor 50000. Buy at 49990.

	// With skew (Anchor 49500):
	// Buy at 49500 - 10 = 49490?
	// But current price is 50000. Grid should probably span around current price, but shifted?
	// The implementation of FindNearestGridPrice uses anchor.
	// nearest = FindNearest(50000, 49500, 10) -> 50000.
	// So grid lines are still aligned to 10s.
	// But the window selection?
	// "Skew prices DOWN (Buy lower, Sell lower)."
	// This usually means we shift the *center* of the grid or the levels themselves.
	// If we use CalculateSkewedPrice on the *Anchor*, then the entire grid shifts.
	// 50000 * 0.99 = 49500.

	// If the grid shifts down by 500, and we are at 50000 (which is 49500 + 500),
	// we are "high" in the grid.
	// Strategy logic:
	// FindNearestGridPrice(50000, 49500, 10) -> 50000.
	// Buy Window: [49990, 49980] (2 levels below 50000).
	// This looks same as unskewed?

	// Ah, the skew should affect the *Current Price* perception or the Grid Levels directly.
	// If we want to buy lower, we need the grid to be lower relative to market.
	// If market is 50000, and we skew anchor to 49500.
	// We are 50 intervals above anchor.
	// We buy at 49990.

	// Maybe I misunderstood the skew logic in context of Grid.
	// If we want to offload inventory, we want to SELL.
	// Selling happens at grid levels ABOVE current.
	// If we shift grid DOWN, 50000 becomes a higher level relative to grid center.
	// So we are more likely to hit Sell levels?
	// In "Long" mode (Directional), we only Place Buy orders.
	// Existing positions (FILLED slots) generate Sell orders (Take Profit).
	// If we shift grid DOWN, the Sell Target for an existing slot (e.g. bought at 49900)
	// usually is Entry + Interval = 49910.
	// Does skew affect TP levels?
	// If not, skew only affects NEW entry levels.

	// If we shift anchor to 49500.
	// New Buy orders will be placed relative to that?
	// No, FindNearestGridPrice aligns to current price.

	// Correct logic for skew:
	// Adjust the *execution* price levels or the *reference* price.
	// "Skew prices DOWN" -> Make buy orders cheaper.
	// If unskewed buy is 49990.
	// Skewed buy should be 49990 * 0.99 = 49490?
	// Or 49990 - shift?

	// Let's assume we modify the Anchor Price used for grid calculation.
	// effectiveAnchor = CalculateSkewedPrice(anchor, inventory...)
	// AND we need to ensure FindNearestGridPrice doesn't just snap back to current market price in a way that negates the shift.
	// Actually FindNearestGridPrice aligns to the grid defined by Anchor.
	// If Anchor=50000, Interval=10. Grid = ..., 49990, 50000, 50010...
	// If Anchor=49995, Interval=10. Grid = ..., 49995, 50005, 50015...
	// So shifting anchor shifts the grid lines.

	// But we want to shift the *Range*.
	// If I have too much inventory, I want to stop buying near 50000 and start buying at 49500.
	// So the "Buy Window" should start lower.
	// Current logic: Buy window starts at `currentGridPrice - interval`.
	// If `currentGridPrice` is close to `currentPrice`, we buy close to market.

	// To implement Trend Following/Inventory Skew:
	// We should adjust the `currentPrice` used to calculate the grid center?
	// Or adjust the `anchorPrice`?
	// If we adjust `currentPrice` passed to FindNearestGridPrice:
	// skewedCurrent = CalculateSkewedPrice(currentPrice, inv, target, skew).
	// If inv > target (10 > 0), skew is negative (0.99).
	// skewedCurrent = 50000 * 0.99 = 49500.
	// nearest = FindNearest(49500, 50000, 10) = 49500.
	// Buy levels: 49490, 49480.
	// Actual market price is 50000.
	// So we are placing buy orders at 49490 (way below market).
	// This achieves "Buy Lower".

	// What about Selling?
	// If we are Neutral, we sell above skewed center (49500).
	// Sell levels: 49510, 49520...
	// Market is 50000.
	// 49510 is below market.
	// We would immediately sell?
	// Yes, if we have inventory, we want to dump it. Selling below market (if allowed/limit) helps execute?
	// Or rather, we place Limit Sells. Limit Sell at 49510 when market is 50000 -> Executes immediately (taker).
	// This fits "Offload inventory".

	// So the plan: Apply skew to the `currentPrice` used for grid centering.

	// Validating in test:
	// We expect generated Buy actions to be around 49500, not 50000.

	foundBuyNear50k := false
	foundBuyNear49500 := false

	for _, a := range actions {
		if a.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE {
			price := pbu.ToGoDecimal(a.Price)
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
