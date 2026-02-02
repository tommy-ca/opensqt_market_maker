package arbitrage

import (
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"math"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestFundingAnalyzer_Analyze(t *testing.T) {
	analyzer := NewFundingAnalyzer()

	// Mock rates: stable positive funding (0.01% per interval)
	// 30 days = 90 intervals
	rates := make([]*pb.FundingRate, 90)
	for i := 0; i < 90; i++ {
		rates[i] = &pb.FundingRate{
			Rate: pbu.FromGoDecimal(decimal.NewFromFloat(0.0001)),
		}
	}

	// Mock candles: stable price (no volatility)
	candles := make([]*pb.Candle, 30)
	for i := 0; i < 30; i++ {
		candles[i] = &pb.Candle{
			Close: pbu.FromGoDecimal(decimal.NewFromInt(100)),
		}
	}

	metrics := analyzer.Analyze(rates, candles)
	if !metrics.Momentum.IsZero() {
		t.Logf("Momentum is not zero: %v", metrics.Momentum)
	}
	sma7, _ := metrics.SMA7d.Float64()
	assert.InDelta(t, 0.0001, sma7, 1e-9)
	sma1, _ := metrics.SMA1d.Float64()
	assert.InDelta(t, 0.0001, sma1, 1e-9)
	posRatio, _ := metrics.PositiveRatio.Float64()
	assert.InDelta(t, 1.0, posRatio, 1e-9)
	assert.Equal(t, 90, metrics.CurrentDuration)
	assert.True(t, metrics.StabilityScore.GreaterThanOrEqual(decimal.NewFromInt(10)))
	assert.True(t, metrics.VolatilityScore.IsZero())

	momentumF, _ := metrics.Momentum.Float64()
	assert.True(t, math.Abs(momentumF) < 1e-10) // Flat line

	// Mock rates: growing funding (positive momentum)
	ratesM := make([]*pb.FundingRate, 10)
	for i := 0; i < 10; i++ {
		ratesM[i] = &pb.FundingRate{
			Rate: pbu.FromGoDecimal(decimal.NewFromFloat(0.0001 * float64(10-i))),
		}
	}
	metricsM := analyzer.Analyze(ratesM, nil)
	assert.True(t, metricsM.Momentum.IsPositive())

	// Mock rates: volatile funding
	ratesV := []*pb.FundingRate{
		{Rate: pbu.FromGoDecimal(decimal.NewFromFloat(0.0001))},
		{Rate: pbu.FromGoDecimal(decimal.NewFromFloat(-0.0001))},
		{Rate: pbu.FromGoDecimal(decimal.NewFromFloat(0.0002))},
	}
	// Volatile price
	candlesV := []*pb.Candle{
		{Close: pbu.FromGoDecimal(decimal.NewFromInt(100))},
		{Close: pbu.FromGoDecimal(decimal.NewFromInt(110))},
		{Close: pbu.FromGoDecimal(decimal.NewFromInt(105))},
	}
	metricsV := analyzer.Analyze(ratesV, candlesV)
	assert.True(t, metricsV.StabilityScore.LessThan(metrics.StabilityScore))
	posRatioV, _ := metricsV.PositiveRatio.Float64()
	assert.InDelta(t, 0.666666, posRatioV, 1e-5)
	assert.True(t, metricsV.VolatilityScore.IsPositive())
}
