package arbitrage

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestComputeSpread(t *testing.T) {
	long := decimal.NewFromFloat(0.0001)  // long leg pays 1 bp
	short := decimal.NewFromFloat(0.0003) // short leg receives 3 bp

	spread := ComputeSpread(long, short)
	if spread.String() != "0.0002" {
		t.Fatalf("expected 0.0002, got %s", spread.String())
	}
}

func TestAnnualizeSpread(t *testing.T) {
	spread := decimal.NewFromFloat(0.0002)   // per interval
	intervalHours := decimal.NewFromFloat(8) // typical perp interval

	apr := AnnualizeSpread(spread, intervalHours)

	// Expect spread * (24/8)*365 = 0.0002 * 3 * 365 = 0.219
	expected := decimal.NewFromFloat(0.219)
	if !apr.Equal(expected) {
		t.Fatalf("expected %s, got %s", expected.String(), apr.String())
	}

	zero := AnnualizeSpread(spread, decimal.Zero)
	if !zero.IsZero() {
		t.Fatalf("expected zero when interval is zero, got %s", zero)
	}
}
