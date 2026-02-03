package grid

import (
	"context"
	"market_maker/internal/pb"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestStrategy_CalculateTargetState(t *testing.T) {
	cfg := StrategyConfig{
		Symbol:         "BTCUSDT",
		Exchange:       "mock",
		PriceInterval:  decimal.NewFromInt(10),
		OrderQuantity:  decimal.NewFromInt(1),
		MinOrderValue:  decimal.NewFromInt(5),
		BuyWindowSize:  2,
		SellWindowSize: 2,
		PriceDecimals:  2,
		QtyDecimals:    3,
		IsNeutral:      true,
	}
	strat := NewGridStrategy(cfg)

	anchorPrice := decimal.NewFromInt(100)
	currentPrice := decimal.NewFromInt(100)
	atr := decimal.Zero

	// Initial state: No slots
	// New logic auto-generates orders for active window (Size 2)
	// Price 100, Interval 10. Buy [90, 80], Sell [110, 120]
	levels := []GridLevel{}
	target, err := strat.CalculateTargetState(context.Background(), currentPrice, anchorPrice, atr, 0, false, false, levels)
	assert.NoError(t, err)
	assert.Len(t, target.Orders, 4)

	levels = []GridLevel{
		{Price: decimal.NewFromInt(90), SlotStatus: pb.SlotStatus_SLOT_STATUS_FREE, PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY},
		{Price: decimal.NewFromInt(110), SlotStatus: pb.SlotStatus_SLOT_STATUS_FREE, PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY},
	}

	target, err = strat.CalculateTargetState(context.Background(), currentPrice, anchorPrice, atr, 0, false, false, levels)
	assert.NoError(t, err)
	assert.Len(t, target.Orders, 4) // 90, 110 (existing) + 80, 120 (new)

	// Test Risk Triggered
	// Should only generate Sells (none if empty, but here we check opening orders are blocked)
	target, err = strat.CalculateTargetState(context.Background(), currentPrice, anchorPrice, atr, 0, true, false, levels)
	assert.NoError(t, err)
	assert.Len(t, target.Orders, 0, "No opening orders should be placed when risk is triggered")
}

func TestStrategy_RiskTriggeredWithPositions(t *testing.T) {
	cfg := StrategyConfig{
		Symbol:         "BTCUSDT",
		Exchange:       "mock",
		PriceInterval:  decimal.NewFromInt(100),
		OrderQuantity:  decimal.NewFromFloat(1),
		MinOrderValue:  decimal.NewFromInt(10),
		BuyWindowSize:  1,
		SellWindowSize: 1,
		PriceDecimals:  2,
		QtyDecimals:    2,
		IsNeutral:      true,
	}

	strat := NewGridStrategy(cfg)

	// Case: Filled Long Position
	levels := []GridLevel{
		{
			Price:          decimal.NewFromInt(9900),
			PositionStatus: pb.PositionStatus_POSITION_STATUS_FILLED,
			PositionQty:    decimal.NewFromInt(1),
		},
	}

	target, err := strat.CalculateTargetState(
		context.Background(),
		decimal.NewFromInt(10000), // currentPrice
		decimal.NewFromInt(10000), // anchorPrice
		decimal.Zero,              // ATR
		1.0,                       // volatilityFactor
		true,                      // isRiskTriggered
		false,                     // isCircuitTripped
		levels,
	)
	assert.NoError(t, err)

	// Should have 1 order (Sell to close position)
	assert.Len(t, target.Orders, 1)
	assert.Equal(t, "SELL", target.Orders[0].Side)
}
