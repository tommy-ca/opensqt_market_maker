package gridengine

import (
	"github.com/shopspring/decimal"
)

// Config holds the configuration for Grid engines
type Config struct {
	StrategyID          string
	Symbol              string
	Exchange            string
	PriceInterval       decimal.Decimal
	OrderQuantity       decimal.Decimal
	MinOrderValue       decimal.Decimal
	BuyWindowSize       int
	SellWindowSize      int
	PriceDecimals       int
	QtyDecimals         int
	IsNeutral           bool
	VolatilityScale     float64
	InventorySkewFactor float64
}
