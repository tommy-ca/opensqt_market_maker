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

// IMarginSimulator is the interface that MarginSim implements, exposed for tools
type IMarginSimulator interface {
	SimulateImpact(proposals map[string]decimal.Decimal) decimal.Decimal // returns HealthScore
	GetRiskProfile() RiskProfile
}
