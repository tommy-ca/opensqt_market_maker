package portfolio

import (
	"market_maker/internal/trading/arbitrage"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestPortfolioAllocator_Allocate(t *testing.T) {
	allocator := NewPortfolioAllocator()

	totalEquity := decimal.NewFromInt(10000)
	leverage := decimal.NewFromInt(3)

	opps := []arbitrage.Opportunity{
		{
			Symbol:       "BTCUSDT",
			QualityScore: decimal.NewFromFloat(0.8),
			Metrics:      arbitrage.FundingMetrics{NarrativeSector: "L1"},
		},
		{
			Symbol:       "ETHUSDT",
			QualityScore: decimal.NewFromFloat(0.6),
			Metrics:      arbitrage.FundingMetrics{NarrativeSector: "L1"},
		},
		{
			Symbol:       "SOLUSDT",
			QualityScore: decimal.NewFromFloat(0.4),
			Metrics:      arbitrage.FundingMetrics{NarrativeSector: "L1"},
		},
		{
			Symbol:       "TAOUSDT",
			QualityScore: decimal.NewFromFloat(0.9),
			Metrics:      arbitrage.FundingMetrics{NarrativeSector: "AI"},
		},
	}

	targets := allocator.Allocate(opps, totalEquity, leverage)

	// TotalScore = 0.8 + 0.6 + 0.4 + 0.9 = 2.7
	// BTC Weight: 0.8 / 2.7 = 0.296 -> Capped at 0.25 (L1 sector)
	// ETH Weight: 0.6 / 2.7 = 0.222 -> Capped at 0.05 (L1 total 0.30 cap)
	// SOL Weight: 0.4 / 2.7 = 0.148 -> Dropped (L1 already at 0.30 cap)
	// TAO Weight: 0.9 / 2.7 = 0.333 -> Capped at 0.25 (AI sector)

	assert.Len(t, targets, 3)

	for _, target := range targets {
		if target.Symbol == "BTCUSDT" {
			assert.Equal(t, "0.25", target.Weight.String())
		}
		if target.Symbol == "ETHUSDT" {
			assert.Equal(t, "0.05", target.Weight.String())
		}
		if target.Symbol == "TAOUSDT" {
			assert.Equal(t, "0.25", target.Weight.String())
		}
	}
}
