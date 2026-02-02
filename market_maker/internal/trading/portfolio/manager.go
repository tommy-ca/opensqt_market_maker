package portfolio

import (
	"context"
	"encoding/json"
	"fmt"
	"market_maker/internal/engine"
	"market_maker/internal/engine/arbengine"
	"market_maker/internal/trading/arbitrage"
	"sync"

	"github.com/shopspring/decimal"
)

// PortfolioManager implements IEngineManager by combining a selector and a factory
type PortfolioManager struct {
	selector *arbitrage.UniverseSelector
	factory  engine.EngineFactory

	mu       sync.RWMutex
	lastOpps map[string]arbitrage.Opportunity
}

func NewPortfolioManager(selector *arbitrage.UniverseSelector, factory engine.EngineFactory) *PortfolioManager {
	return &PortfolioManager{
		selector: selector,
		factory:  factory,
		lastOpps: make(map[string]arbitrage.Opportunity),
	}
}

func (m *PortfolioManager) Scan(ctx context.Context) ([]arbitrage.Opportunity, error) {
	opps, err := m.selector.Scan(ctx)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.lastOpps = make(map[string]arbitrage.Opportunity)
	for _, o := range opps {
		m.lastOpps[o.Symbol] = o
	}
	m.mu.Unlock()

	return opps, nil
}

func (m *PortfolioManager) CreateConfig(symbol string, notional decimal.Decimal) (json.RawMessage, error) {
	m.mu.RLock()
	opp, ok := m.lastOpps[symbol]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no known opportunity for symbol %s", symbol)
	}

	config := arbengine.EngineConfig{
		Symbol:        symbol,
		SpotExchange:  opp.LongExchange,
		PerpExchange:  opp.ShortExchange,
		OrderQuantity: notional,
		// Default thresholds
		MinSpreadAPR:  decimal.NewFromFloat(0.10),
		ExitSpreadAPR: decimal.NewFromFloat(0.01),
	}
	return json.Marshal(config)
}

func (m *PortfolioManager) CreateEngine(symbol string, config json.RawMessage) (engine.Engine, error) {
	return m.factory.CreateEngine(symbol, config)
}
