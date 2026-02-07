package margin

import (
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"sync"

	"github.com/shopspring/decimal"
)

// MarginSim provides precision margin simulation
type MarginSim struct {
	mu sync.RWMutex

	lastAccount *pb.Account
	prices      map[string]decimal.Decimal
	haircuts    map[string]decimal.Decimal // asset -> weight (0.0 to 1.0)
	mmrs        map[string]decimal.Decimal // symbol -> maintenance margin rate

	safetyBuffer decimal.Decimal // e.g., 0.20 for 20%
	defaultMMR   decimal.Decimal
}

func NewMarginSim() *MarginSim {
	return &MarginSim{
		prices:       make(map[string]decimal.Decimal),
		haircuts:     make(map[string]decimal.Decimal),
		mmrs:         make(map[string]decimal.Decimal),
		safetyBuffer: decimal.NewFromFloat(0.20),
		defaultMMR:   decimal.NewFromFloat(0.05), // Default 5% MMR
	}
}

func (s *MarginSim) UpdateAccount(acc *pb.Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAccount = acc

	// Harvest haircuts and MMRs from positions if available
	for _, pos := range acc.Positions {
		if pos.Haircut != nil {
			s.haircuts[pos.Symbol] = pbu.ToGoDecimal(pos.Haircut)
		}
		if pos.MarkPrice != nil {
			s.prices[pos.Symbol] = pbu.ToGoDecimal(pos.MarkPrice)
		}
		// In UM, MMR is often standard per symbol.
		// If the proto had MMR, we'd harvest it here.
	}
}

func (s *MarginSim) UpdatePrice(symbol string, price decimal.Decimal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prices[symbol] = price
}

func (s *MarginSim) SetHaircut(asset string, weight decimal.Decimal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.haircuts[asset] = weight
}

func (s *MarginSim) SetMMR(symbol string, rate decimal.Decimal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mmrs[symbol] = rate
}

func (s *MarginSim) GetRiskProfile() core.RiskProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.lastAccount == nil {
		return core.RiskProfile{}
	}

	acc := s.lastAccount
	adjEq := pbu.ToGoDecimal(acc.AdjustedEquity)
	maintMargin := pbu.ToGoDecimal(acc.TotalMaintenanceMargin)

	// If AdjustedEquity is missing, fallback to TotalMarginBalance
	if adjEq.IsZero() {
		adjEq = pbu.ToGoDecimal(acc.TotalMarginBalance)
	}

	headroom := adjEq.Sub(maintMargin)
	if headroom.IsNegative() {
		headroom = decimal.Zero
	}

	// Apply internal safety buffer to headroom
	safeHeadroom := headroom.Mul(decimal.NewFromInt(1).Sub(s.safetyBuffer))

	return core.RiskProfile{
		AdjustedEquity:         adjEq,
		TotalMaintenanceMargin: maintMargin,
		AvailableHeadroom:      safeHeadroom,
		HealthScore:            pbu.ToGoDecimal(acc.HealthScore),
		IsUnified:              acc.IsUnified,
	}
}

// SimulateImpact checks the margin impact of proposed position changes
func (s *MarginSim) SimulateImpact(proposals map[string]decimal.Decimal) core.SimulationResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.lastAccount == nil {
		return core.SimulationResult{
			HealthScore:    decimal.Zero,
			WouldLiquidate: true,
		}
	}

	adjEq := pbu.ToGoDecimal(s.lastAccount.AdjustedEquity)
	tmm := pbu.ToGoDecimal(s.lastAccount.TotalMaintenanceMargin)

	// Fallback for AdjustedEquity if it's zero/missing
	if adjEq.IsZero() {
		adjEq = pbu.ToGoDecimal(s.lastAccount.TotalMarginBalance)
	}

	projectedAdjEq := adjEq
	projectedTMM := tmm

	for symbol, delta := range proposals {
		if delta.IsZero() {
			continue
		}

		price, ok := s.prices[symbol]
		if !ok {
			// Cannot simulate without price, return 0 health for safety
			return core.SimulationResult{
				HealthScore:                decimal.Zero,
				ProjectedAdjustedEquity:    projectedAdjEq,
				ProjectedMaintenanceMargin: projectedTMM,
				WouldLiquidate:             true,
			}
		}

		// 1. Maintenance Margin Impact (MMR)
		mmr, hasMMR := s.mmrs[symbol]
		if !hasMMR {
			mmr = s.defaultMMR
		}

		currentSize := decimal.Zero
		for _, pos := range s.lastAccount.Positions {
			if pos.Symbol == symbol {
				currentSize = pbu.ToGoDecimal(pos.Size)
				break
			}
		}
		newSize := currentSize.Add(delta)

		oldMaint := currentSize.Abs().Mul(price).Mul(mmr)
		newMaint := newSize.Abs().Mul(price).Mul(mmr)
		projectedTMM = projectedTMM.Add(newMaint.Sub(oldMaint))

		// 2. Adjusted Equity Impact (ECV/Haircut)
		haircut, hasHaircut := s.haircuts[symbol]
		if !hasHaircut {
			// If it's a stablecoin, it might not have a haircut record but should be 1.0.
			// However, for safety in UM simulation, we require the haircut to be set.
			// Fallback to 0 if not found to be conservative? Or 1.0 for stables?
			// Let's be conservative: if missing, assume 0.0 haircut (no collateral value).
			haircut = decimal.Zero
		}

		// Change in AdjEq = delta * price * (haircut - 1.0)
		impact := delta.Mul(price).Mul(haircut.Sub(decimal.NewFromInt(1)))
		projectedAdjEq = projectedAdjEq.Add(impact)
	}

	if projectedAdjEq.IsPositive() {
		// HealthScore = 1 - (TMM * (1 + SafetyBuffer) / AdjEq)
		safeTMM := projectedTMM.Mul(decimal.NewFromInt(1).Add(s.safetyBuffer))
		health := decimal.NewFromInt(1).Sub(safeTMM.Div(projectedAdjEq))

		if health.IsNegative() {
			return core.SimulationResult{
				HealthScore:                decimal.Zero,
				ProjectedAdjustedEquity:    projectedAdjEq,
				ProjectedMaintenanceMargin: projectedTMM,
				WouldLiquidate:             true,
			}
		}

		finalHealth := health
		if health.GreaterThan(decimal.NewFromInt(1)) {
			finalHealth = decimal.NewFromInt(1)
		}

		return core.SimulationResult{
			HealthScore:                finalHealth,
			ProjectedAdjustedEquity:    projectedAdjEq,
			ProjectedMaintenanceMargin: projectedTMM,
			WouldLiquidate:             false,
		}
	}

	return core.SimulationResult{
		HealthScore:                decimal.Zero,
		ProjectedAdjustedEquity:    projectedAdjEq,
		ProjectedMaintenanceMargin: projectedTMM,
		WouldLiquidate:             true,
	}
}
