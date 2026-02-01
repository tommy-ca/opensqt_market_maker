package arbitrage_test

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/internal/trading/arbitrage"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestUniverseManager_Refresh(t *testing.T) {
	// Setup Exchanges
	spotEx := mock.NewMockExchange("binance_spot")
	perpEx := mock.NewMockExchange("binance")

	exchanges := map[string]core.IExchange{
		"binance_spot": spotEx,
		"binance":      perpEx,
	}

	selector := arbitrage.NewUniverseSelector(exchanges)
	selector.AddStrategy("SpotPerp", "binance_spot", "binance")

	manager := arbitrage.NewUniverseManager(selector, &mockLogger{}, time.Hour)

	// Mock Data for BTCUSDT
	spotEx.SetFundingRate("BTCUSDT", decimal.Zero)
	perpEx.SetFundingRate("BTCUSDT", decimal.NewFromFloat(0.0001))

	btcRates := make([]*pb.FundingRate, 90)
	for i := 0; i < 90; i++ {
		btcRates[i] = &pb.FundingRate{Rate: pbu.FromGoDecimal(decimal.NewFromFloat(0.0001))}
	}
	perpEx.SetHistoricalFundingRates("BTCUSDT", btcRates)

	ctx := context.Background()
	err := manager.Refresh(ctx)
	assert.NoError(t, err)

	// Active target should be BTCUSDT
	assert.Equal(t, "BTCUSDT", manager.GetActiveTarget())
	assert.NotEmpty(t, manager.GetTopOpportunities())
}

func TestUniverseManager_SwitchingLogic(t *testing.T) {
	// Setup
	spotEx := mock.NewMockExchange("binance_spot")
	perpEx := mock.NewMockExchange("binance")
	exchanges := map[string]core.IExchange{"binance_spot": spotEx, "binance": perpEx}
	selector := arbitrage.NewUniverseSelector(exchanges)
	selector.AddStrategy("SpotPerp", "binance_spot", "binance")
	manager := arbitrage.NewUniverseManager(selector, &mockLogger{}, time.Hour)

	// Switch Params: 0.3% cost, 7 days hold, 1.5 buffer -> Threshold = 0.0045 (Gain > 0.45% over 7 days)
	manager.SetSwitchingParams(decimal.NewFromFloat(0.003), decimal.NewFromInt(7), decimal.NewFromFloat(1.5))

	// ETH: 15% APR stable (Rank 1 initially)
	spotEx.SetFundingRate("ETHUSDT", decimal.Zero)
	perpEx.SetFundingRate("ETHUSDT", decimal.NewFromFloat(0.00015)) // ~16.4% APR
	ethRates := make([]*pb.FundingRate, 90)
	for i := 0; i < 90; i++ {
		ethRates[i] = &pb.FundingRate{Rate: pbu.FromGoDecimal(decimal.NewFromFloat(0.00015))}
	}
	perpEx.SetHistoricalFundingRates("ETHUSDT", ethRates)

	// BTC: 10% APR stable (Rank 2 initially)
	spotEx.SetFundingRate("BTCUSDT", decimal.Zero)
	perpEx.SetFundingRate("BTCUSDT", decimal.NewFromFloat(0.0001)) // ~11% APR
	btcRates := make([]*pb.FundingRate, 90)
	for i := 0; i < 90; i++ {
		btcRates[i] = &pb.FundingRate{Rate: pbu.FromGoDecimal(decimal.NewFromFloat(0.0001))}
	}
	perpEx.SetHistoricalFundingRates("BTCUSDT", btcRates)

	ctx := context.Background()

	// First Scan: Pick ETH
	err := manager.Refresh(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "ETHUSDT", manager.GetActiveTarget())

	// CASE 1: BTC becomes slightly better than ETH, but gain is low.
	// ETH APR -> 9% (0.000082)
	// BTC APR -> 11% (0.0001)
	// Diff = 2% APR. Gain = 0.02 * 7 / 365 = 0.00038.  Threshold = 0.0045.
	// Should NOT switch.
	perpEx.SetFundingRate("ETHUSDT", decimal.NewFromFloat(0.000082))
	for i := 0; i < 90; i++ {
		ethRates[i].Rate = pbu.FromGoDecimal(decimal.NewFromFloat(0.000082))
	}

	err = manager.Refresh(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "ETHUSDT", manager.GetActiveTarget(), "Should NOT switch due to low gain")

	// CASE 2: BTC becomes much better.
	// BTC APR -> 50% (0.00045)
	// Diff = 50% - 9% = 41%. Gain = 0.41 * 7 / 365 = 0.0078. Threshold = 0.0045.
	// Should SWITCH.
	selector.ClearCache()
	perpEx.SetFundingRate("BTCUSDT", decimal.NewFromFloat(0.00045))
	for i := 0; i < 90; i++ {
		btcRates[i].Rate = pbu.FromGoDecimal(decimal.NewFromFloat(0.00045))
	}

	err = manager.Refresh(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "BTCUSDT", manager.GetActiveTarget(), "Should SWITCH due to high gain")
}
