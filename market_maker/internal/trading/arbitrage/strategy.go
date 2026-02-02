package arbitrage

import (
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"

	"github.com/shopspring/decimal"
)

// StrategyConfig holds the configuration for arbitrage logic
type StrategyConfig struct {
	MinSpreadAPR         decimal.Decimal
	ExitSpreadAPR        decimal.Decimal
	LiquidationThreshold decimal.Decimal
	UMWarningThreshold   decimal.Decimal // 0.7
	UMEmergencyThreshold decimal.Decimal // 0.5
	ToxicBasisThreshold  decimal.Decimal // e.g., -0.0005 (-5 bps)
}

// ArbitrageAction represents a decision made by the strategy
type ArbitrageAction int

const (
	ActionNone ArbitrageAction = iota
	ActionEntryPositive
	ActionEntryNegative
	ActionExit
	ActionReduceExposure // New: Partial reduction for UM health
	ActionToxicExit      // New: Exit due to toxic basis
)

// Strategy implements the pure logic for arbitrage
type Strategy struct {
	cfg StrategyConfig
}

func NewStrategy(cfg StrategyConfig) *Strategy {
	// Defaults if not provided
	if cfg.UMWarningThreshold.IsZero() {
		cfg.UMWarningThreshold = decimal.NewFromFloat(0.7)
	}
	if cfg.UMEmergencyThreshold.IsZero() {
		cfg.UMEmergencyThreshold = decimal.NewFromFloat(0.5)
	}
	return &Strategy{cfg: cfg}
}

// CalculateAction decides whether to enter, exit or do nothing.
// Input spreadAPR should be already annualized.
func (s *Strategy) CalculateAction(spreadAPR decimal.Decimal, isPositionOpen bool) ArbitrageAction {
	if !isPositionOpen {
		if spreadAPR.GreaterThan(s.cfg.MinSpreadAPR) {
			return ActionEntryPositive
		}
		if spreadAPR.LessThan(s.cfg.MinSpreadAPR.Neg()) {
			return ActionEntryNegative
		}
	}

	if isPositionOpen {
		// Exit if spread within exit threshold (around zero)
		if spreadAPR.Abs().LessThan(s.cfg.ExitSpreadAPR) {
			return ActionExit
		}
		// Also exit if spread flips sign significantly?
		// For now simple exit threshold.
	}

	return ActionNone
}

// ShouldEmergencyExit checks if liquidation risk is too high (standard futures)
func (s *Strategy) ShouldEmergencyExit(pos *pb.Position) bool {
	markPrice := pbu.ToGoDecimal(pos.MarkPrice)
	liqPrice := pbu.ToGoDecimal(pos.LiquidationPrice)
	size := pbu.ToGoDecimal(pos.Size)

	if liqPrice.IsZero() || size.IsZero() {
		return false
	}

	var distance decimal.Decimal
	if size.IsPositive() {
		distance = markPrice.Sub(liqPrice).Div(markPrice).Abs()
	} else {
		distance = liqPrice.Sub(markPrice).Div(markPrice).Abs()
	}

	return distance.LessThan(s.cfg.LiquidationThreshold)
}

// EvaluateUMAccountHealth returns an action based on Unified Margin health score
func (s *Strategy) EvaluateUMAccountHealth(account *pb.Account) ArbitrageAction {
	if account == nil || !account.IsUnified {
		return ActionNone
	}

	health := pbu.ToGoDecimal(account.HealthScore)
	if health.IsZero() && account.HealthScore != nil {
		// Possibly 0 means absolute liquidation risk
		return ActionExit
	}

	if health.LessThan(s.cfg.UMEmergencyThreshold) {
		return ActionExit
	}

	if health.LessThan(s.cfg.UMWarningThreshold) {
		return ActionReduceExposure
	}

	return ActionNone
}

// EvaluateBasis checks if the spot-perp basis is toxic
func (s *Strategy) EvaluateBasis(spotPrice, perpPrice decimal.Decimal) ArbitrageAction {
	if spotPrice.IsZero() || perpPrice.IsZero() || s.cfg.ToxicBasisThreshold.IsZero() {
		return ActionNone
	}

	// basis = (perp - spot) / spot
	basis := perpPrice.Sub(spotPrice).Div(spotPrice)

	if basis.LessThan(s.cfg.ToxicBasisThreshold) {
		return ActionToxicExit
	}

	return ActionNone
}
