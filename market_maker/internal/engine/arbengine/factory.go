package arbengine

import (
	"encoding/json"
	"market_maker/internal/core"
	"market_maker/internal/engine"
)

// ArbitrageEngineFactory implements engine.EngineFactory for Arbitrage engines
type ArbitrageEngineFactory struct {
	exchanges      map[string]core.IExchange
	riskMonitor    core.IRiskMonitor
	fundingMonitor core.IFundingMonitor
	logger         core.ILogger
}

func NewArbitrageEngineFactory(
	exchanges map[string]core.IExchange,
	riskMonitor core.IRiskMonitor,
	fundingMonitor core.IFundingMonitor,
	logger core.ILogger,
) *ArbitrageEngineFactory {
	return &ArbitrageEngineFactory{
		exchanges:      exchanges,
		riskMonitor:    riskMonitor,
		fundingMonitor: fundingMonitor,
		logger:         logger,
	}
}

func (f *ArbitrageEngineFactory) CreateEngine(symbol string, config json.RawMessage) (engine.Engine, error) {
	var cfg EngineConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return nil, err
	}
	// Override symbol from call if needed, but usually it's in config
	if cfg.Symbol == "" {
		cfg.Symbol = symbol
	}
	return NewArbitrageEngine(f.exchanges, f.riskMonitor, f.fundingMonitor, f.logger, cfg), nil
}
