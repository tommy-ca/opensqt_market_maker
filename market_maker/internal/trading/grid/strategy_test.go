package grid_test

import (
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/internal/trading/grid"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestStrategy_CalculateActions(t *testing.T) {
	cfg := grid.StrategyConfig{
		Symbol:         "BTCUSDT",
		PriceInterval:  decimal.NewFromInt(10),
		OrderQuantity:  decimal.NewFromInt(1),
		MinOrderValue:  decimal.NewFromInt(5),
		BuyWindowSize:  2,
		SellWindowSize: 2,
		PriceDecimals:  2,
		QtyDecimals:    3,
		IsNeutral:      true,
	}
	strat := grid.NewStrategy(cfg)

	anchorPrice := decimal.NewFromInt(100)
	currentPrice := decimal.NewFromInt(100)
	atr := decimal.Zero

	// Initial state: No slots
	// New logic auto-generates orders for active window (Size 2)
	// Price 100, Interval 10. Buy [90, 80], Sell [110, 120]
	slots := []core.StrategySlot{}
	actions := strat.CalculateActions(currentPrice, anchorPrice, atr, 0, false, pb.MarketRegime_MARKET_REGIME_RANGE, slots)
	assert.Len(t, actions, 4)

	slots = []core.StrategySlot{
		{Price: decimal.NewFromInt(90), SlotStatus: pb.SlotStatus_SLOT_STATUS_FREE, PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY},
		{Price: decimal.NewFromInt(110), SlotStatus: pb.SlotStatus_SLOT_STATUS_FREE, PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY},
	}

	actions = strat.CalculateActions(currentPrice, anchorPrice, atr, 0, false, pb.MarketRegime_MARKET_REGIME_RANGE, slots)
	assert.Len(t, actions, 4) // 90, 110 (existing) + 80, 120 (new)

	// Test Risk Triggered
	// Should only generate Sells (110, 120)
	actions = strat.CalculateActions(currentPrice, anchorPrice, atr, 0, true, pb.MarketRegime_MARKET_REGIME_RANGE, slots)
	assert.Len(t, actions, 2, "Only Sell orders should be placed when risk is triggered")
	assert.Equal(t, pb.OrderSide_ORDER_SIDE_SELL, actions[0].Request.Side)

}

func TestStrategy_RiskTriggeredCancelsLockedBuys(t *testing.T) {
	cfg := grid.StrategyConfig{
		Symbol:         "BTCUSDT",
		PriceInterval:  decimal.NewFromInt(100),
		OrderQuantity:  decimal.NewFromFloat(1),
		MinOrderValue:  decimal.NewFromInt(10),
		BuyWindowSize:  1,
		SellWindowSize: 1,
		PriceDecimals:  2,
		QtyDecimals:    2,
		IsNeutral:      true,
	}

	strat := grid.NewStrategy(cfg)
	lockedBuy := core.StrategySlot{
		Price:          decimal.NewFromInt(9800),
		SlotStatus:     pb.SlotStatus_SLOT_STATUS_LOCKED,
		OrderSide:      pb.OrderSide_ORDER_SIDE_BUY,
		OrderPrice:     decimal.NewFromInt(9800),
		PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY,
	}

	actions := strat.CalculateActions(
		decimal.NewFromInt(10000), // currentPrice
		decimal.NewFromInt(10000), // anchorPrice
		decimal.Zero,              // ATR
		1.0,                       // volatilityFactor
		true,                      // isRiskTriggered
		pb.MarketRegime_MARKET_REGIME_RANGE,
		[]core.StrategySlot{lockedBuy},
	)

	foundCancel := false
	for _, a := range actions {
		if a.Type == pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL {
			foundCancel = true
		}
		if a.Request != nil && a.Request.Side == pb.OrderSide_ORDER_SIDE_BUY {
			t.Fatalf("risk triggered should not place BUY orders, found: %+v", a)
		}
	}

	if !foundCancel {
		t.Fatalf("expected at least one CANCEL action when risk is triggered")
	}
}
