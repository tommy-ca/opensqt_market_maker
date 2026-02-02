package arbengine

import (
	"time"

	"github.com/shopspring/decimal"
)

// EngineConfig holds the configuration for Arbitrage engines
type EngineConfig struct {
	Symbol                    string
	SpotExchange              string
	PerpExchange              string
	OrderQuantity             decimal.Decimal
	MinSpreadAPR              decimal.Decimal
	ExitSpreadAPR             decimal.Decimal
	LiquidationThreshold      decimal.Decimal
	UMWarningThreshold        decimal.Decimal
	UMEmergencyThreshold      decimal.Decimal
	ToxicBasisThreshold       decimal.Decimal
	FundingStalenessThreshold time.Duration
}
