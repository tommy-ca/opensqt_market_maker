package arbitrage_test

import (
	"market_maker/internal/pb"
	"market_maker/internal/trading/arbitrage"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestStrategy_CalculateAction(t *testing.T) {
	cfg := arbitrage.StrategyConfig{
		MinSpreadAPR:  decimal.NewFromFloat(0.10), // 10%
		ExitSpreadAPR: decimal.NewFromFloat(0.01), // 1%
	}
	strat := arbitrage.NewStrategy(cfg)

	tests := []struct {
		name           string
		spreadAPR      decimal.Decimal
		isPositionOpen bool
		expected       arbitrage.ArbitrageAction
	}{
		{
			name:           "Opportunity found, position closed -> Entry",
			spreadAPR:      decimal.NewFromFloat(0.15), // 15%
			isPositionOpen: false,
			expected:       arbitrage.ActionEntryPositive,
		},
		{
			name:           "Opportunity found, position already open -> None",
			spreadAPR:      decimal.NewFromFloat(0.15),
			isPositionOpen: true,
			expected:       arbitrage.ActionNone,
		},
		{
			name:           "Spread low, position open -> Exit",
			spreadAPR:      decimal.NewFromFloat(0.005), // 0.5%
			isPositionOpen: true,
			expected:       arbitrage.ActionExit,
		},
		{
			name:           "Spread low, position closed -> None",
			spreadAPR:      decimal.NewFromFloat(0.005),
			isPositionOpen: false,
			expected:       arbitrage.ActionNone,
		},
		{
			name:           "Spread negative, position open -> Exit",
			spreadAPR:      decimal.NewFromFloat(-0.005), // -0.5%
			isPositionOpen: true,
			expected:       arbitrage.ActionExit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := strat.CalculateAction(tt.spreadAPR, tt.isPositionOpen)
			assert.Equal(t, tt.expected, action)
		})
	}
}

func TestStrategy_ShouldEmergencyExit(t *testing.T) {
	cfg := arbitrage.StrategyConfig{
		LiquidationThreshold: decimal.NewFromFloat(0.10), // 10%
	}
	strat := arbitrage.NewStrategy(cfg)

	tests := []struct {
		name     string
		pos      *pb.Position
		expected bool
	}{
		{
			name: "Long position, far from liq -> false",
			pos: &pb.Position{
				Size:             pbu.FromGoDecimal(decimal.NewFromFloat(1.0)),
				MarkPrice:        pbu.FromGoDecimal(decimal.NewFromInt(100)),
				LiquidationPrice: pbu.FromGoDecimal(decimal.NewFromInt(80)), // 20% away
			},
			expected: false,
		},
		{
			name: "Long position, close to liq -> true",
			pos: &pb.Position{
				Size:             pbu.FromGoDecimal(decimal.NewFromFloat(1.0)),
				MarkPrice:        pbu.FromGoDecimal(decimal.NewFromInt(100)),
				LiquidationPrice: pbu.FromGoDecimal(decimal.NewFromInt(95)), // 5% away
			},
			expected: true,
		},
		{
			name: "Short position, far from liq -> false",
			pos: &pb.Position{
				Size:             pbu.FromGoDecimal(decimal.NewFromFloat(-1.0)),
				MarkPrice:        pbu.FromGoDecimal(decimal.NewFromInt(100)),
				LiquidationPrice: pbu.FromGoDecimal(decimal.NewFromInt(120)), // 20% away
			},
			expected: false,
		},
		{
			name: "Short position, close to liq -> true",
			pos: &pb.Position{
				Size:             pbu.FromGoDecimal(decimal.NewFromFloat(-1.0)),
				MarkPrice:        pbu.FromGoDecimal(decimal.NewFromInt(100)),
				LiquidationPrice: pbu.FromGoDecimal(decimal.NewFromInt(105)), // 5% away
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := strat.ShouldEmergencyExit(tt.pos)
			assert.Equal(t, tt.expected, result)
		})
	}
}
