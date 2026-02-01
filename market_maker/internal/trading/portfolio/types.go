package portfolio

import (
	"context"
	"encoding/json"
	"market_maker/internal/engine"
	"market_maker/internal/trading/arbitrage"

	"github.com/shopspring/decimal"
)

// IEngineManager handles scanning for opportunities and creating engine instances
type IEngineManager interface {
	engine.EngineFactory
	Scan(ctx context.Context) ([]arbitrage.Opportunity, error)
	CreateConfig(symbol string, notional decimal.Decimal) (json.RawMessage, error)
}

// IOrchestrator defines the methods needed from the orchestrator
type IOrchestrator interface {
	AddSymbol(symbol string, eng engine.Engine)
	RemoveSymbol(symbol string)
	StartSymbol(symbol string) error
	GetEngine(symbol string) (engine.Engine, bool)
	GetSymbols() []string

	// Persistent Intent Operations
	AddTradingPair(ctx context.Context, symbol string, exchange string, config json.RawMessage, targetNotional decimal.Decimal, qualityScore decimal.Decimal, sector string) error
	RemoveTradingPair(ctx context.Context, symbol string) error
}

// PortfolioEngine is an engine that supports dynamic resizing
type PortfolioEngine interface {
	engine.Engine
	SetOrderQuantity(qty decimal.Decimal)
	GetOrderQuantity() decimal.Decimal
}

// TargetPosition represents the desired state for a symbol
type TargetPosition struct {
	Symbol       string
	Weight       decimal.Decimal
	Notional     decimal.Decimal
	Exchange     string
	QualityScore decimal.Decimal
}

// RebalanceAction defines an adjustment for a symbol
type RebalanceAction struct {
	Symbol   string
	Priority int // 1: Cancel, 2: De-risk, 3: Rebalance, 4: Entry
	Diff     decimal.Decimal
}

// PortfolioOpportunity aggregates scoring and metadata
type PortfolioOpportunity struct {
	arbitrage.Opportunity
	Weight decimal.Decimal
}
