package arbitrage

import "github.com/shopspring/decimal"

// ComputeSpread returns the funding spread (short - long) for a symbol.
// Inputs are per-interval funding rates (not annualized).
func ComputeSpread(longRate, shortRate decimal.Decimal) decimal.Decimal {
	return shortRate.Sub(longRate)
}

// AnnualizeSpread converts a per-interval spread to APR using the interval duration (hours).
// If intervalHours is zero or negative, returns zero to avoid division by zero.
func AnnualizeSpread(spread decimal.Decimal, intervalHours decimal.Decimal) decimal.Decimal {
	if intervalHours.Sign() <= 0 {
		return decimal.Zero
	}
	periodsPerYear := decimal.NewFromInt(365 * 24).Div(intervalHours) // 365 days * 24h / interval
	return spread.Mul(periodsPerYear)
}
