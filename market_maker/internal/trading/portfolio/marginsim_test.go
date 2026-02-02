package portfolio

import (
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestMarginSim_GetRiskProfile(t *testing.T) {
	sim := NewMarginSim()

	// 1. Initial State
	assert.True(t, sim.GetRiskProfile().AdjustedEquity.IsZero())

	// 2. Update Account
	acc := &pb.Account{
		IsUnified:              true,
		AdjustedEquity:         pbu.FromGoDecimal(decimal.NewFromInt(10000)),
		TotalMaintenanceMargin: pbu.FromGoDecimal(decimal.NewFromInt(2000)),
		HealthScore:            pbu.FromGoDecimal(decimal.NewFromFloat(0.8)),
	}
	sim.UpdateAccount(acc)

	profile := sim.GetRiskProfile()
	assert.True(t, profile.IsUnified)
	assert.Equal(t, "10000", profile.AdjustedEquity.String())
	assert.Equal(t, "2000", profile.TotalMaintenanceMargin.String())

	// Headroom = 10000 - 2000 = 8000
	// Safe Headroom = 8000 * (1 - 0.20) = 6400
	assert.Equal(t, "6400", profile.AvailableHeadroom.String())
}

func TestMarginSim_SimulateImpact(t *testing.T) {
	sim := NewMarginSim()
	// 20% safety buffer is default

	// Initial State: $10k equity, $1k maintenance margin
	acc := &pb.Account{
		AdjustedEquity:         pbu.FromGoDecimal(decimal.NewFromInt(10000)),
		TotalMaintenanceMargin: pbu.FromGoDecimal(decimal.NewFromInt(1000)),
	}
	sim.UpdateAccount(acc)
	sim.UpdatePrice("BTCUSDT", decimal.NewFromInt(50000))
	sim.SetMMR("BTCUSDT", decimal.NewFromFloat(0.01)) // 1% MMR
	sim.SetHaircut("BTCUSDT", decimal.NewFromInt(1))  // 100% collateral value

	// 1. Current Health (Base)
	// Base Health = 1 - (1000 * 1.20 / 10000) = 0.88
	baseHealth := sim.SimulateImpact(nil)
	baseHealthF, _ := baseHealth.Float64()
	assert.InDelta(t, 0.88, baseHealthF, 0.0001)

	// 2. Open 0.1 BTC Position (Delta = +0.1, Current = 0)
	// New TMM = 1000 + |0.1|*50000*0.01 = 1050
	// Delta AdjEq = 0.1 * 50000 * (1 - 1) = 0
	// Health = 1 - (1050 * 1.20 / 10000) = 1 - 1260 / 10000 = 0.874
	impact := sim.SimulateImpact(map[string]decimal.Decimal{
		"BTCUSDT": decimal.NewFromFloat(0.1),
	})
	impactF, _ := impact.Float64()
	assert.InDelta(t, 0.874, impactF, 0.0001)

	// 3. Reduce Position (Delta = -0.02, Current = 0.1)
	acc.Positions = []*pb.Position{
		{Symbol: "BTCUSDT", Size: pbu.FromGoDecimal(decimal.NewFromFloat(0.1))},
	}
	acc.TotalMaintenanceMargin = pbu.FromGoDecimal(decimal.NewFromInt(1050))
	sim.UpdateAccount(acc)

	// Delta TMM = |0.1-0.02|*500 - |0.1|*500 = 40 - 50 = -10
	// New TMM = 1050 - 10 = 1040
	// Health = 1 - (1040 * 1.20 / 10000) = 1 - 1248 / 10000 = 0.8752
	impact = sim.SimulateImpact(map[string]decimal.Decimal{
		"BTCUSDT": decimal.NewFromFloat(-0.02),
	})
	impactF, _ = impact.Float64()
	assert.InDelta(t, 0.8752, impactF, 0.0001)

	// 4. Spot Haircut Impact
	sim.UpdatePrice("ETH", decimal.NewFromInt(2000))
	sim.SetHaircut("ETH", decimal.NewFromFloat(0.9))
	sim.SetMMR("ETH", decimal.Zero) // Spot has 0 MMR

	// Buy 10 ETH (Delta = 10)
	// Delta AdjEq = 10 * 2000 * (0.9 - 1.0) = -2000
	// New AdjEq = 10000 - 2000 = 8000
	// Current TMM = 1050 (from last UpdateAccount)
	// Health = 1 - (1050 * 1.20 / 8000) = 1 - 1260 / 8000 = 1 - 0.1575 = 0.8425
	impact = sim.SimulateImpact(map[string]decimal.Decimal{
		"ETH": decimal.NewFromInt(10),
	})
	impactF, _ = impact.Float64()
	assert.InDelta(t, 0.8425, impactF, 0.0001)
}

func TestMarginSim_SimulateImpact_Fallback(t *testing.T) {
	sim := NewMarginSim()
	acc := &pb.Account{
		TotalMarginBalance:     pbu.FromGoDecimal(decimal.NewFromInt(5000)),
		TotalMaintenanceMargin: pbu.FromGoDecimal(decimal.NewFromInt(500)),
	}
	sim.UpdateAccount(acc)

	// Base Health = 1 - (500 * 1.20 / 5000) = 1 - 600 / 5000 = 1 - 0.12 = 0.88
	health := sim.SimulateImpact(nil)
	healthF, _ := health.Float64()
	assert.InDelta(t, 0.88, healthF, 0.0001)
}
