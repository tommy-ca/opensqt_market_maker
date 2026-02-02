package portfolio

import (
	"market_maker/internal/trading/arbitrage"

	"github.com/shopspring/decimal"
)

// PortfolioAllocator calculates target weights for a portfolio of arbitrage opportunities
type PortfolioAllocator struct {
	maxPairs       int
	maxWeight      decimal.Decimal
	sectorCap      decimal.Decimal // e.g. 0.20 for 20%
	roundTripCost  decimal.Decimal // e.g. 0.003 for 0.3%
	hysteresisMult decimal.Decimal // e.g. 2.5
}

func NewPortfolioAllocator() *PortfolioAllocator {
	return &PortfolioAllocator{
		maxPairs:       10,
		maxWeight:      decimal.NewFromFloat(0.25), // Max 25% per pair
		sectorCap:      decimal.NewFromFloat(0.30), // Max 30% per narrative sector
		roundTripCost:  decimal.NewFromFloat(0.003),
		hysteresisMult: decimal.NewFromFloat(2.5),
	}
}

// Allocate computes target weights based on QualityScores and risk constraints
func (a *PortfolioAllocator) Allocate(opps []arbitrage.Opportunity, totalEquity decimal.Decimal, leverage decimal.Decimal) []TargetPosition {
	if len(opps) == 0 {
		return nil
	}

	// 1. Filter and Limit pairs
	limit := a.maxPairs
	if len(opps) < limit {
		limit = len(opps)
	}
	topOpps := opps[:limit]

	// 2. Calculate initial weights using Linear Scoring
	totalScore := decimal.Zero
	for _, o := range topOpps {
		if o.QualityScore.IsPositive() {
			totalScore = totalScore.Add(o.QualityScore)
		}
	}

	if totalScore.IsZero() {
		return nil
	}

	// 3. Apply Sector Caps and Normalized Weights
	sectorExposure := make(map[string]decimal.Decimal)
	var targets []TargetPosition

	totalCapacity := totalEquity.Mul(leverage)

	for _, o := range topOpps {
		if !o.QualityScore.IsPositive() {
			continue
		}

		rawWeight := o.QualityScore.Div(totalScore)

		// Apply per-pair cap
		if rawWeight.GreaterThan(a.maxWeight) {
			rawWeight = a.maxWeight
		}

		// Apply sector cap
		sector := o.Metrics.NarrativeSector
		if sector == "" {
			sector = "unknown"
		}

		currentSectorExp := sectorExposure[sector]
		if currentSectorExp.Add(rawWeight).GreaterThan(a.sectorCap) {
			rawWeight = a.sectorCap.Sub(currentSectorExp)
			if rawWeight.IsNegative() {
				rawWeight = decimal.Zero
			}
		}

		if rawWeight.IsPositive() {
			targets = append(targets, TargetPosition{
				Symbol:       o.Symbol,
				Weight:       rawWeight,
				Notional:     totalCapacity.Mul(rawWeight),
				Exchange:     o.ShortExchange, // Assume Perp exchange as primary for tracking
				QualityScore: o.QualityScore,
			})
			sectorExposure[sector] = currentSectorExp.Add(rawWeight)
		}
	}

	return targets
}

// ShouldRebalance checks if the gain from switching state justifies the cost
func (a *PortfolioAllocator) ShouldRebalance(currentNotional, targetNotional decimal.Decimal, annualizedYield decimal.Decimal) bool {
	diff := targetNotional.Sub(currentNotional).Abs()
	if diff.IsZero() {
		return false
	}

	// Hysteresis rule: Gap (Entry - Exit) >= 2.5 * Roundtrip Costs
	threshold := a.roundTripCost.Mul(a.hysteresisMult)

	// For now, use a simple relative weight change threshold of 5%
	return diff.Div(currentNotional.Add(decimal.NewFromInt(1))).GreaterThan(decimal.NewFromFloat(0.05)) ||
		diff.GreaterThan(threshold.Mul(currentNotional.Add(decimal.NewFromInt(1))))
}
