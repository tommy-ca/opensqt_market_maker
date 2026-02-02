package arbitrage

import (
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestVirtualMarginEngine(t *testing.T) {
	vme := NewVirtualMarginEngine()

	// 1. Initial State
	assert.True(t, vme.EstimateHealth().IsZero())

	// 2. Update Account
	acc := &pb.Account{
		IsUnified:              true,
		AdjustedEquity:         pbu.FromGoDecimal(decimal.NewFromFloat(10000)),
		TotalMaintenanceMargin: pbu.FromGoDecimal(decimal.NewFromFloat(1000)),
	}
	vme.UpdateAccount(acc)
	// Health = 1 - (1000 * 1.2 / 10000) = 0.88
	assert.Equal(t, "0.88", vme.EstimateHealth().String())

	// 3. Update Price
	// Initial state: AdjEq=10000, TMM=1000 (implied by 0.85 health? No, health is reported as 0.85)
	// Let's set up a concrete account for VME testing
	acc = &pb.Account{
		IsUnified:              true,
		AdjustedEquity:         pbu.FromGoDecimal(decimal.NewFromInt(10000)),
		TotalMaintenanceMargin: pbu.FromGoDecimal(decimal.NewFromInt(1000)),
		Positions: []*pb.Position{
			{
				Symbol:    "BTCUSDT",
				Size:      pbu.FromGoDecimal(decimal.NewFromFloat(0.1)),
				MarkPrice: pbu.FromGoDecimal(decimal.NewFromInt(50000)),
				Haircut:   pbu.FromGoDecimal(decimal.NewFromFloat(1.0)),
			},
		},
	}
	vme.UpdateAccount(acc)

	// Baseline: 1 - (1000 * 1.2 / 10000) = 1 - 0.12 = 0.88
	assert.Equal(t, "0.88", vme.EstimateHealth().String())

	// Price drops: BTC 50000 -> 40000
	// Delta Eq = 0.1 * (40000 - 50000) = -1000
	// New Est AdjEq = 10000 - 1000 = 9000
	// New Health = 1 - (1000 * 1.2 / 9000) = 1 - 1200 / 9000 = 1 - 0.1333... = 0.8666...
	vme.UpdatePrice("BTCUSDT", decimal.NewFromInt(40000))
	health := vme.EstimateHealth()
	assert.True(t, health.LessThan(decimal.NewFromFloat(0.87)))
	assert.True(t, health.GreaterThan(decimal.NewFromFloat(0.86)))
}

func TestStrategy_EvaluateUMAccountHealth(t *testing.T) {
	cfg := StrategyConfig{
		UMWarningThreshold:   decimal.NewFromFloat(0.7),
		UMEmergencyThreshold: decimal.NewFromFloat(0.5),
	}
	s := NewStrategy(cfg)

	// Normal
	acc := &pb.Account{
		IsUnified:   true,
		HealthScore: pbu.FromGoDecimal(decimal.NewFromFloat(0.8)),
	}
	assert.Equal(t, ActionNone, s.EvaluateUMAccountHealth(acc))

	// Warning
	acc.HealthScore = pbu.FromGoDecimal(decimal.NewFromFloat(0.65))
	assert.Equal(t, ActionReduceExposure, s.EvaluateUMAccountHealth(acc))

	// Emergency
	acc.HealthScore = pbu.FromGoDecimal(decimal.NewFromFloat(0.45))
	assert.Equal(t, ActionExit, s.EvaluateUMAccountHealth(acc))
}
