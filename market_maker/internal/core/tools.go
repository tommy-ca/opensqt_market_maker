package core

import (
	"github.com/shopspring/decimal"
)

// PositionProposal represents a proposed change in position
type PositionProposal struct {
	Symbol string
	Size   decimal.Decimal
}

// RiskProfile represents the current risk state of the account
type RiskProfile struct {
	AdjustedEquity         decimal.Decimal
	TotalMaintenanceMargin decimal.Decimal
	AvailableHeadroom      decimal.Decimal
	HealthScore            decimal.Decimal
	IsUnified              bool
}

// SimulationResult holds the result of a margin simulation
type SimulationResult struct {
	HealthScore                decimal.Decimal
	ProjectedAdjustedEquity    decimal.Decimal
	ProjectedMaintenanceMargin decimal.Decimal
	WouldLiquidate             bool
}

// IMarginSimulator is the interface that MarginSim implements, exposed for tools
type IMarginSimulator interface {
	SimulateImpact(proposals map[string]decimal.Decimal) SimulationResult
	GetRiskProfile() RiskProfile
}
