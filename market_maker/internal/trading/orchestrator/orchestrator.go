package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/engine"
	"market_maker/internal/pb"
	"sync"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"github.com/shopspring/decimal"
)

// SymbolManager represents an isolated vertical trading slice for one symbol
type SymbolManager struct {
	symbol    string
	engine    engine.Engine
	priceChan chan *pb.PriceChange
	orderChan chan *pb.OrderUpdate
	logger    core.ILogger
	ctx       context.Context
	cancel    context.CancelFunc
}

func NewSymbolManager(symbol string, eng engine.Engine, logger core.ILogger) *SymbolManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &SymbolManager{
		symbol:    symbol,
		engine:    eng,
		priceChan: make(chan *pb.PriceChange, 100),
		orderChan: make(chan *pb.OrderUpdate, 100),
		logger:    logger.WithField("symbol", symbol),
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (m *SymbolManager) Start() error {
	go m.run()
	return nil
}

func (m *SymbolManager) Stop() {
	m.cancel()
}

func (m *SymbolManager) run() {
	defer func() {
		if r := recover(); r != nil {
			m.logger.Error("Symbol manager panicked", "panic", r)
		}
	}()

	m.logger.Info("Starting symbol manager loop")
	for {
		select {
		case <-m.ctx.Done():
			return
		case p := <-m.priceChan:
			if err := m.engine.OnPriceUpdate(m.ctx, p); err != nil {
				m.logger.Error("Failed to process price update", "error", err)
			}
		case o := <-m.orderChan:
			if err := m.engine.OnOrderUpdate(m.ctx, o); err != nil {
				m.logger.Error("Failed to process order update", "error", err)
			}
		}
	}
}

// Orchestrator manages multiple SymbolManagers and shared resources
type Orchestrator struct {
	exchange  core.IExchange
	managers  map[string]*SymbolManager
	mu        sync.RWMutex
	logger    core.ILogger
	factory   engine.EngineFactory
	workflows *OrchestratorWorkflows
	dbosCtx   dbos.DBOSContext
}

func NewOrchestrator(exch core.IExchange, factory engine.EngineFactory, logger core.ILogger) *Orchestrator {
	return &Orchestrator{
		exchange: exch,
		managers: make(map[string]*SymbolManager),
		logger:   logger.WithField("component", "orchestrator"),
		factory:  factory,
	}
}

func (o *Orchestrator) SetDBOS(ctx dbos.DBOSContext, w *OrchestratorWorkflows) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.dbosCtx = ctx
	o.workflows = w
}

func (o *Orchestrator) AddTradingPair(ctx context.Context, symbol string, exchange string, config json.RawMessage, targetNotional decimal.Decimal, qualityScore decimal.Decimal, sector string) error {
	o.mu.RLock()
	dbosCtx := o.dbosCtx
	workflows := o.workflows
	o.mu.RUnlock()

	if dbosCtx == nil || workflows == nil {
		o.logger.Warn("DBOS not initialized, skipping persistence", "symbol", symbol)
		return nil
	}

	entry := RegistryEntry{
		Symbol:         symbol,
		Exchange:       exchange,
		Config:         config,
		Status:         "ACTIVE",
		TargetNotional: targetNotional,
		QualityScore:   qualityScore,
		Sector:         sector,
	}

	_, err := dbosCtx.RunWorkflow(dbosCtx, func(ctx dbos.DBOSContext, input any) (any, error) {
		return workflows.AddTradingPair(ctx, input)
	}, entry)
	return err
}

func (o *Orchestrator) RemoveTradingPair(ctx context.Context, symbol string) error {
	o.mu.RLock()
	dbosCtx := o.dbosCtx
	workflows := o.workflows
	o.mu.RUnlock()

	if dbosCtx == nil || workflows == nil {
		o.logger.Warn("DBOS not initialized, skipping persistence", "symbol", symbol)
		return nil
	}

	_, err := dbosCtx.RunWorkflow(dbosCtx, func(ctx dbos.DBOSContext, input any) (any, error) {
		return workflows.RemoveTradingPair(ctx, input)
	}, symbol)
	return err
}

func (o *Orchestrator) AddSymbol(symbol string, eng engine.Engine) {
	o.mu.Lock()
	defer o.mu.Unlock()

	manager := NewSymbolManager(symbol, eng, o.logger)
	o.managers[symbol] = manager
}

func (o *Orchestrator) StartSymbol(symbol string) error {
	o.mu.RLock()
	m, ok := o.managers[symbol]
	o.mu.RUnlock()

	if !ok {
		return fmt.Errorf("symbol manager not found for %s", symbol)
	}
	return m.Start()
}

func (o *Orchestrator) RemoveSymbol(symbol string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if m, ok := o.managers[symbol]; ok {
		m.Stop()
		delete(o.managers, symbol)
	}
}

func (o *Orchestrator) GetEngine(symbol string) (engine.Engine, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	m, ok := o.managers[symbol]
	if !ok {
		return nil, false
	}
	return m.engine, true
}

func (o *Orchestrator) GetSymbols() []string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	var symbols []string
	for s := range o.managers {
		symbols = append(symbols, s)
	}
	return symbols
}

func (o *Orchestrator) Start(ctx context.Context) error {
	o.mu.RLock()
	defer o.mu.RUnlock()

	for _, m := range o.managers {
		if err := m.Start(); err != nil {
			return err
		}
	}

	// Start global streams
	if err := o.exchange.StartOrderStream(ctx, o.routeOrderUpdate); err != nil {
		return err
	}

	// For price streams, we aggregate all symbols into one multiplexed stream
	var symbols []string
	for symbol := range o.managers {
		symbols = append(symbols, symbol)
	}

	if len(symbols) > 0 {
		if err := o.exchange.StartPriceStream(ctx, symbols, o.routePriceUpdate); err != nil {
			return err
		}
	}

	return nil
}

func (o *Orchestrator) routePriceUpdate(p *pb.PriceChange) {
	// Ensure exchange is set (adapters might not set it if they don't know which instance they are)
	if p.Exchange == "" {
		p.Exchange = o.exchange.GetName()
	}

	o.mu.RLock()
	m, ok := o.managers[p.Symbol]
	o.mu.RUnlock()

	if ok {
		select {
		case m.priceChan <- p:
		default:
			o.logger.Warn("Price channel full for symbol", "symbol", p.Symbol)
		}
	}
}

func (o *Orchestrator) routeOrderUpdate(u *pb.OrderUpdate) {
	o.mu.RLock()
	m, ok := o.managers[u.Symbol]
	o.mu.RUnlock()

	if ok {
		select {
		case m.orderChan <- u:
		default:
			o.logger.Warn("Order channel full for symbol", "symbol", u.Symbol)
		}
	}
}
