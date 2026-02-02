package arbitrage_test

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/internal/trading/arbitrage"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestUniverseSelector_Scan(t *testing.T) {
	// Setup Exchanges
	spotEx := mock.NewMockExchange("binance_spot")
	perpEx := mock.NewMockExchange("binance")

	exchanges := map[string]core.IExchange{
		"binance_spot": spotEx,
		"binance":      perpEx,
	}

	// Setup Selector with strategy: Spot-Perp on Binance
	// We want to find BTCUSDT where Perp Funding is High Positive
	selector := arbitrage.NewUniverseSelector(exchanges)
	selector.AddStrategy("SpotPerp", "binance_spot", "binance")

	// Mock Data
	// Perp Funding Rate = 0.05% (0.0005) -> ~54% APR (3 * 365)
	spotEx.SetFundingRate("BTCUSDT", decimal.Zero)
	perpEx.SetFundingRate("BTCUSDT", decimal.NewFromFloat(0.0005))

	ctx := context.Background()

	opps, err := selector.Scan(ctx)
	assert.NoError(t, err)
	assert.NotEmpty(t, opps)

	opp := opps[0]
	assert.Equal(t, "BTCUSDT", opp.Symbol)
	assert.Equal(t, "binance_spot", opp.LongExchange) // Long Spot
	assert.Equal(t, "binance", opp.ShortExchange)     // Short Perp

	// Expected Spread: 0.0001 (Perp Rate) - 0 (Spot Rate) = 0.0001
	// APR: 0.0001 * 3 * 365 = 0.1095 (10.95%)
	assert.True(t, opp.SpreadAPR.GreaterThan(decimal.Zero))
}

func TestUniverseSelector_QualityRanking(t *testing.T) {
	// Setup Exchanges
	spotEx := mock.NewMockExchange("binance_spot")
	perpEx := mock.NewMockExchange("binance")

	exchanges := map[string]core.IExchange{
		"binance_spot": spotEx,
		"binance":      perpEx,
	}

	selector := arbitrage.NewUniverseSelector(exchanges)
	selector.AddStrategy("SpotPerp", "binance_spot", "binance")

	// Symbols: BTCUSDT (Stable, High Score), ETHUSDT (Volatile, Lower Score)
	ctx := context.Background()

	// BTCUSDT: 0.01% stable
	spotEx.SetFundingRate("BTCUSDT", decimal.Zero)
	perpEx.SetFundingRate("BTCUSDT", decimal.NewFromFloat(0.0001))

	// ETHUSDT: 0.02% currently, but SMA might be lower or more volatile
	spotEx.SetFundingRate("ETHUSDT", decimal.Zero)
	perpEx.SetFundingRate("ETHUSDT", decimal.NewFromFloat(0.0002))

	// Mock History for BTC (Stable)
	btcRates := make([]*pb.FundingRate, 90)
	for i := 0; i < 90; i++ {
		btcRates[i] = &pb.FundingRate{Rate: pbu.FromGoDecimal(decimal.NewFromFloat(0.0001))}
	}
	perpEx.SetHistoricalFundingRates("BTCUSDT", btcRates)

	// Mock History for ETH (Volatile/Declining)
	ethRates := make([]*pb.FundingRate, 90)
	for i := 0; i < 90; i++ {
		// Declining trend: 0.0002 -> 0.0001
		rate := 0.0001 + (float64(90-i)/90.0)*0.0001
		ethRates[i] = &pb.FundingRate{Rate: pbu.FromGoDecimal(decimal.NewFromFloat(rate))}
	}
	perpEx.SetHistoricalFundingRates("ETHUSDT", ethRates)

	opps, err := selector.Scan(ctx)
	assert.NoError(t, err)
	assert.Len(t, opps, 2)

	// BTC should be ranked higher due to stability/consistent yield even if ETH has higher snapshot APR
	// (This depends on exact weights, but let's verify sorting works)
	assert.Equal(t, "BTCUSDT", opps[0].Symbol)
	assert.Equal(t, "ETHUSDT", opps[1].Symbol)
}

func TestUniverseSelector_HistoryCache(t *testing.T) {
	spotEx := mock.NewMockExchange("binance_spot")
	perpEx := mock.NewMockExchange("binance")

	exchanges := map[string]core.IExchange{
		"binance_spot": spotEx,
		"binance":      perpEx,
	}

	selector := arbitrage.NewUniverseSelector(exchanges)
	selector.AddStrategy("SpotPerp", "binance_spot", "binance")

	ctx := context.Background()
	symbol := "BTCUSDT"

	spotEx.SetFundingRate(symbol, decimal.Zero)
	perpEx.SetFundingRate(symbol, decimal.NewFromFloat(0.0001))

	// Initial Scan - Should fetch and cache
	opps1, err := selector.Scan(ctx)
	assert.NoError(t, err)
	assert.Len(t, opps1, 1)

	// Change historical rates on exchange - but cache should still return old ones
	newRates := make([]*pb.FundingRate, 90)
	for i := 0; i < 90; i++ {
		newRates[i] = &pb.FundingRate{Rate: pbu.FromGoDecimal(decimal.NewFromFloat(0.0009))}
	}
	perpEx.SetHistoricalFundingRates(symbol, newRates)

	// Second Scan - Should use cache
	opps2, err := selector.Scan(ctx)
	assert.NoError(t, err)
	assert.Len(t, opps2, 1)

	// If cached, SMA7d should be based on old rates (0.0001 from default mock)
	// mock.GetHistoricalFundingRates returns 0.0001 by default if not set.
	// Since we didn't set it before the first scan, it cached 0.0001.
	sma7, _ := opps2[0].Metrics.SMA7d.Float64()
	assert.InDelta(t, 0.0001, sma7, 0.000001)
}
