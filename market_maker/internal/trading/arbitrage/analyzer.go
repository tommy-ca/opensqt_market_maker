package arbitrage

import (
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"math"

	"github.com/shopspring/decimal"
)

// FundingMetrics contains analyzed historical data for a symbol
type FundingMetrics struct {
	SMA1d            decimal.Decimal
	SMA7d            decimal.Decimal
	SMA30d           decimal.Decimal
	StabilityScore   decimal.Decimal // Mean / StdDev of funding
	VolatilityScore  decimal.Decimal // StdDev of price returns
	Momentum         decimal.Decimal // Slope of funding trend
	OIFactor         decimal.Decimal // OI / Volume ratio
	PositiveRatio    decimal.Decimal // % of intervals with rate > 0
	NumSignFlips     int             // Total flips in history
	CurrentDuration  int             // Consecutive intervals with current sign
	AverageAnnualAPR decimal.Decimal
	NarrativeSector  string // e.g. "L1", "DeFi", "AI"
}

// FundingAnalyzer provides utilities for historical funding analysis
type FundingAnalyzer struct{}

func NewFundingAnalyzer() *FundingAnalyzer {
	return &FundingAnalyzer{}
}

// Analyze processes historical rates and returns metrics
func (a *FundingAnalyzer) Analyze(rates []*pb.FundingRate, candles []*pb.Candle) FundingMetrics {
	if len(rates) == 0 {
		return FundingMetrics{}
	}

	sum := decimal.Zero
	var positiveCount int

	for _, r := range rates {
		val := pbu.ToGoDecimal(r.Rate)
		sum = sum.Add(val)
		if val.IsPositive() {
			positiveCount++
		}
	}

	count := decimal.NewFromInt(int64(len(rates)))
	mean := sum.Div(count)

	// Positive Ratio
	posRatio := decimal.NewFromInt(int64(positiveCount)).Div(count)

	// Stability (Mean / StdDev) - Uses float64 for StdDev math
	var varianceSum float64
	meanF, _ := mean.Float64()
	for _, r := range rates {
		val, _ := pbu.ToGoDecimal(r.Rate).Float64()
		varianceSum += math.Pow(val-meanF, 2)
	}
	countF := float64(len(rates))
	stdDev := math.Sqrt(varianceSum / countF)

	stability := decimal.NewFromInt(10) // Default high stability if stdDev is 0
	if stdDev > 0 {
		stability = mean.Abs().Div(decimal.NewFromFloat(stdDev))
	}

	// Current Duration and Sign Flips
	duration := 0
	flips := 0
	if len(rates) > 0 {
		firstVal := pbu.ToGoDecimal(rates[0].Rate)
		currentSign := firstVal.IsPositive()
		lastSign := currentSign
		for i, r := range rates {
			val := pbu.ToGoDecimal(r.Rate)
			sign := val.IsPositive()

			// Duration count for newest regime
			if flips == 0 && sign == currentSign {
				duration++
			}

			// Flip detection
			if i > 0 && sign != lastSign {
				flips++
			}
			lastSign = sign
		}
	}

	// SMA cycles (assuming 8h intervals)
	sma1 := a.calculateSMA(rates, 3)  // 1 day
	sma7 := a.calculateSMA(rates, 21) // 7 days
	sma30 := mean                     // full sample (usually 30d)

	// Momentum (Slope of last 10 intervals)
	momentum := a.calculateMomentum(rates, 10)

	// Price Volatility - Uses float64 for Log/Sqrt
	volatility := a.calculateVolatility(candles)

	return FundingMetrics{
		SMA1d:            sma1,
		SMA7d:            sma7,
		SMA30d:           sma30,
		StabilityScore:   stability,
		VolatilityScore:  decimal.NewFromFloat(volatility),
		Momentum:         momentum,
		PositiveRatio:    posRatio,
		NumSignFlips:     flips,
		CurrentDuration:  duration,
		AverageAnnualAPR: sma7.Mul(decimal.NewFromInt(3)).Mul(decimal.NewFromInt(365)),
	}
}

func (a *FundingAnalyzer) calculateSMA(rates []*pb.FundingRate, period int) decimal.Decimal {
	if len(rates) == 0 {
		return decimal.Zero
	}
	if len(rates) < period {
		period = len(rates)
	}

	sum := decimal.Zero
	for i := 0; i < period; i++ {
		sum = sum.Add(pbu.ToGoDecimal(rates[i].Rate))
	}
	return sum.Div(decimal.NewFromInt(int64(period)))
}

func (a *FundingAnalyzer) calculateMomentum(rates []*pb.FundingRate, period int) decimal.Decimal {
	if len(rates) < 2 {
		return decimal.Zero
	}
	if len(rates) < period {
		period = len(rates)
	}

	n := decimal.NewFromInt(int64(period))
	sumX := decimal.Zero
	sumY := decimal.Zero
	sumXY := decimal.Zero
	sumX2 := decimal.Zero

	for i := 0; i < period; i++ {
		x := decimal.NewFromInt(int64(period - 1 - i))
		y := pbu.ToGoDecimal(rates[i].Rate)

		sumX = sumX.Add(x)
		sumY = sumY.Add(y)
		sumXY = sumXY.Add(x.Mul(y))
		sumX2 = sumX2.Add(x.Mul(x))
	}

	denom := n.Mul(sumX2).Sub(sumX.Mul(sumX))
	if denom.IsZero() {
		return decimal.Zero
	}

	return n.Mul(sumXY).Sub(sumX.Mul(sumY)).Div(denom)
}

func (a *FundingAnalyzer) calculateVolatility(candles []*pb.Candle) float64 {
	if len(candles) < 2 {
		return 0
	}

	returns := make([]float64, 0, len(candles)-1)
	for i := 1; i < len(candles); i++ {
		cPrev, _ := pbu.ToGoDecimal(candles[i-1].Close).Float64()
		cCurr, _ := pbu.ToGoDecimal(candles[i].Close).Float64()
		if cPrev == 0 {
			continue
		}
		returns = append(returns, math.Log(cCurr/cPrev))
	}

	if len(returns) == 0 {
		return 0
	}

	var sum float64
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	var varSum float64
	for _, r := range returns {
		varSum += math.Pow(r-mean, 2)
	}
	return math.Sqrt(varSum / float64(len(returns)))
}
