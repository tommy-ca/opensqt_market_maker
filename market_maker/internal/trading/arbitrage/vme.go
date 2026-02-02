package arbitrage

import (
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"sync"

	"github.com/shopspring/decimal"
)

// VirtualMarginEngine estimates account health based on real-time price updates
type VirtualMarginEngine struct {
	mu sync.RWMutex

	lastAccount *pb.Account
	prices      map[string]decimal.Decimal
	haircuts    map[string]decimal.Decimal // asset -> haircut (0.0 to 1.0, 1.0 means no haircut)
}

func NewVirtualMarginEngine() *VirtualMarginEngine {
	return &VirtualMarginEngine{
		prices:   make(map[string]decimal.Decimal),
		haircuts: make(map[string]decimal.Decimal),
	}
}

// UpdateAccount updates the base account snapshot
func (v *VirtualMarginEngine) UpdateAccount(acc *pb.Account) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.lastAccount = acc
}

// UpdatePrice updates the real-time price for an asset/symbol
func (v *VirtualMarginEngine) UpdatePrice(symbol string, price decimal.Decimal) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.prices[symbol] = price
}

// SetHaircut sets the collateral weight for an asset
func (v *VirtualMarginEngine) SetHaircut(asset string, weight decimal.Decimal) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.haircuts[asset] = weight
}

// EstimateHealth returns an estimated health score based on latest prices
func (v *VirtualMarginEngine) EstimateHealth() decimal.Decimal {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.lastAccount == nil || !v.lastAccount.IsUnified {
		return decimal.Zero
	}

	// Baseline values from last snapshot
	adjEq := pbu.ToGoDecimal(v.lastAccount.AdjustedEquity)
	tmm := pbu.ToGoDecimal(v.lastAccount.TotalMaintenanceMargin)

	if adjEq.IsZero() {
		adjEq = pbu.ToGoDecimal(v.lastAccount.TotalMarginBalance)
	}

	// Calculate Estimated Equity Change
	deltaEq := decimal.Zero
	for _, pos := range v.lastAccount.Positions {
		if currentPrice, ok := v.prices[pos.Symbol]; ok {
			markPrice := pbu.ToGoDecimal(pos.MarkPrice)
			size := pbu.ToGoDecimal(pos.Size)

			// PnL Delta = size * (currentPrice - markPrice)
			pnlDelta := size.Mul(currentPrice.Sub(markPrice))

			// Apply haircut if present (collateral value change)
			haircut := decimal.NewFromInt(1)
			if !pbu.ToGoDecimal(pos.Haircut).IsZero() {
				haircut = pbu.ToGoDecimal(pos.Haircut)
			} else if h, ok := v.haircuts[pos.Symbol]; ok {
				haircut = h
			}

			deltaEq = deltaEq.Add(pnlDelta.Mul(haircut))
		}
	}

	estAdjEq := adjEq.Add(deltaEq)
	if estAdjEq.IsPositive() {
		// HealthScore = 1 - (TMM * (1 + SafetyBuffer) / AdjustedEquity)
		// Using 20% safety buffer as per requirements
		safetyBuffer := decimal.NewFromFloat(0.20)
		safeTMM := tmm.Mul(decimal.NewFromInt(1).Add(safetyBuffer))

		health := decimal.NewFromInt(1).Sub(safeTMM.Div(estAdjEq))
		if health.IsNegative() {
			return decimal.Zero
		}
		return health
	}

	return decimal.Zero
}

// TODO: Implement more precise ECV (Effective Collateral Value) calculation
// if the pb.Account is extended to include full asset balances.
