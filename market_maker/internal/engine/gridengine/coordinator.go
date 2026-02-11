package gridengine

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/internal/trading/grid"
	"market_maker/internal/trading/monitor"
	"market_maker/pkg/pbu"
	"sort"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// IGridExecutor defines how the coordinator executes actions
type IGridExecutor interface {
	Execute(ctx context.Context, actions []*pb.OrderAction)
}

// GridCoordinator handles the shared logic between Simple and Durable grid engines
type GridCoordinator struct {
	symbol        string
	exchange      core.IExchange
	strategy      *grid.Strategy
	slotManager   core.IPositionManager
	monitor       core.IRiskMonitor
	regimeMonitor *monitor.RegimeMonitor
	store         core.IStateStore
	logger        core.ILogger
	executor      IGridExecutor

	anchorPrice     decimal.Decimal
	lastPrice       decimal.Decimal
	isRiskTriggered bool
	isDirty         bool
	lastSaveTime    time.Time
	stratSlots      []core.StrategySlot
	mu              sync.Mutex
}

// GridCoordinatorDeps encapsulates dependencies for GridCoordinator
type GridCoordinatorDeps struct {
	Cfg         Config
	Exchanges   map[string]core.IExchange
	SlotMgr     core.IPositionManager
	RiskMonitor core.IRiskMonitor
	Store       core.IStateStore
	Logger      core.ILogger
	Executor    IGridExecutor
}

func NewGridCoordinator(deps GridCoordinatorDeps) *GridCoordinator {
	var exch core.IExchange

	// Try to find configured exchange first
	if deps.Cfg.Exchange != "" {
		if e, ok := deps.Exchanges[deps.Cfg.Exchange]; ok {
			exch = e
		}
	}

	// Fallback to deterministic selection if not found or not configured
	if exch == nil && len(deps.Exchanges) > 0 {
		keys := make([]string, 0, len(deps.Exchanges))
		for k := range deps.Exchanges {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		exch = deps.Exchanges[keys[0]]

		// Log warning if exchange was configured but not found
		if deps.Cfg.Exchange != "" {
			deps.Logger.Warn("Configured exchange not found, falling back to deterministic selection",
				"configured", deps.Cfg.Exchange,
				"selected", keys[0])
		}
	}

	strategyCfg := grid.StrategyConfig{
		StrategyID:          deps.Cfg.StrategyID,
		Symbol:              deps.Cfg.Symbol,
		PriceInterval:       deps.Cfg.PriceInterval,
		OrderQuantity:       deps.Cfg.OrderQuantity,
		MinOrderValue:       deps.Cfg.MinOrderValue,
		BuyWindowSize:       deps.Cfg.BuyWindowSize,
		SellWindowSize:      deps.Cfg.SellWindowSize,
		PriceDecimals:       deps.Cfg.PriceDecimals,
		QtyDecimals:         deps.Cfg.QtyDecimals,
		IsNeutral:           deps.Cfg.IsNeutral,
		VolatilityScale:     deps.Cfg.VolatilityScale,
		InventorySkewFactor: deps.Cfg.InventorySkewFactor,
	}

	rm := monitor.NewRegimeMonitor(exch, deps.Logger, deps.Cfg.Symbol)

	return &GridCoordinator{
		symbol:        deps.Cfg.Symbol,
		exchange:      exch,
		strategy:      grid.NewStrategy(strategyCfg),
		slotManager:   deps.SlotMgr,
		monitor:       deps.RiskMonitor,
		regimeMonitor: rm,
		store:         deps.Store,
		logger:        deps.Logger,
		executor:      deps.Executor,
		lastPrice:     decimal.Zero,
		isDirty:       false,
		lastSaveTime:  time.Time{}, // Allow immediate first save
	}
}

// SetRegimeMonitor allows injecting a mock monitor for testing
func (c *GridCoordinator) SetRegimeMonitor(rm interface{}) {
	if r, ok := rm.(*monitor.RegimeMonitor); ok {
		c.regimeMonitor = r
	}
}

func (c *GridCoordinator) Start(ctx context.Context) error {
	c.logger.Info("Starting Grid Coordinator", "symbol", c.symbol)

	// 1. Start Monitors
	if c.regimeMonitor != nil {
		if err := c.regimeMonitor.Start(ctx); err != nil {
			return fmt.Errorf("failed to start regime monitor: %w", err)
		}
	}

	if c.monitor != nil {
		if err := c.monitor.Start(ctx); err != nil {
			return fmt.Errorf("failed to start risk monitor: %w", err)
		}
	}

	// 2. Restore Local State (Warm Boot)
	state, err := c.store.LoadState(ctx)
	if err != nil {
		return fmt.Errorf("failed to load state from store: %w", err)
	}

	if state != nil {
		if err := c.slotManager.RestoreState(state.Slots); err != nil {
			return fmt.Errorf("failed to restore state in slot manager: %w", err)
		}
		c.anchorPrice = pbu.ToGoDecimal(state.LastPrice)
		c.slotManager.SetAnchorPrice(c.anchorPrice)
		c.lastPrice = pbu.ToGoDecimal(state.LastPrice)
		c.isRiskTriggered = state.IsRiskTriggered
		c.logger.Info("Local state restored", "slots", len(state.Slots), "anchor", c.anchorPrice, "risk_triggered", c.isRiskTriggered)
	}

	// 3. Exchange Reconciliation (The Reality Check)
	if c.exchange != nil {
		c.logger.Info("Reconciling with exchange...")

		var openOrders []*pb.Order
		var positions []*pb.Position
		var ordersErr, posErr error
		var wg sync.WaitGroup

		wg.Add(2)
		go func() {
			defer wg.Done()
			openOrders, ordersErr = c.exchange.GetOpenOrders(ctx, c.symbol, false)
		}()

		go func() {
			defer wg.Done()
			positions, posErr = c.exchange.GetPositions(ctx, c.symbol)
		}()

		wg.Wait()

		if ordersErr != nil {
			return fmt.Errorf("failed to fetch open orders during boot: %w", ordersErr)
		}

		if posErr != nil {
			return fmt.Errorf("failed to fetch positions during boot: %w", posErr)
		}

		totalPos := decimal.Zero
		for _, p := range positions {
			totalPos = totalPos.Add(pbu.ToGoDecimal(p.Size))
		}
		c.logger.Info("Current exchange position", "size", totalPos)

		c.slotManager.SyncOrders(openOrders, totalPos)
		c.logger.Info("Exchange reconciliation complete", "open_orders", len(openOrders))

		c.slotManager.RestoreFromExchangePosition(totalPos)
	}

	return nil
}

func (c *GridCoordinator) Stop() error {
	c.logger.Info("Stopping Grid Coordinator")

	var errs []error

	if c.regimeMonitor != nil {
		if err := c.regimeMonitor.Stop(); err != nil {
			c.logger.Error("Failed to stop regime monitor", "error", err)
			errs = append(errs, err)
		}
	}

	if c.monitor != nil {
		if err := c.monitor.Stop(); err != nil {
			c.logger.Error("Failed to stop risk monitor", "error", err)
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping coordinator: %v", errs)
	}
	return nil
}

func (c *GridCoordinator) OnPriceUpdate(ctx context.Context, price *pb.PriceChange) error {
	var riskActions []*pb.OrderAction
	var strategyActions []*pb.OrderAction
	var pVal decimal.Decimal

	// Phase 1: State access and calculation (Locked)
	c.mu.Lock()
	pVal = pbu.ToGoDecimal(price.Price)

	if c.anchorPrice.IsZero() {
		c.anchorPrice = pVal
	}

	// 1. Get ATR and Risk Status
	atr := decimal.Zero
	volFactor := 0.0
	isTriggeredNow := false
	if c.monitor != nil {
		atr = c.monitor.GetATR(price.Symbol)
		volFactor = c.monitor.GetVolatilityFactor(price.Symbol)
		isTriggeredNow = c.monitor.IsTriggered()
	}

	// 2. Handle Risk Transition
	if isTriggeredNow && !c.isRiskTriggered {
		c.logger.Warn("Risk Triggered! Canceling all Buy orders")
		var err error
		riskActions, err = c.slotManager.CancelAllBuyOrders(ctx)
		if err != nil {
			c.mu.Unlock()
			return fmt.Errorf("failed to cancel buy orders on risk trigger: %w", err)
		}
	}
	c.isRiskTriggered = isTriggeredNow

	// 3. Optimized Strategy Actions collection
	c.stratSlots = c.slotManager.GetStrategySlots(c.stratSlots)

	// 4. Get Current Regime
	currentRegime := pb.MarketRegime_MARKET_REGIME_RANGE
	if c.regimeMonitor != nil {
		currentRegime = c.regimeMonitor.GetRegime()
	}

	strategyActions = c.strategy.CalculateActions(pVal, c.anchorPrice, atr, volFactor, isTriggeredNow, currentRegime, c.stratSlots)

	if len(strategyActions) > 0 {
		c.slotManager.MarkSlotsPending(strategyActions)
	}
	if len(riskActions) > 0 {
		c.slotManager.MarkSlotsPending(riskActions)
	}

	c.mu.Unlock()

	// Phase 2: Execution (Unlocked - prevents blocking hot path)
	if len(riskActions) > 0 {
		c.executor.Execute(ctx, riskActions)
	}
	if len(strategyActions) > 0 {
		c.executor.Execute(ctx, strategyActions)
	}

	// Phase 3: Persistence
	c.mu.Lock()
	c.lastPrice = pVal
	if len(strategyActions) > 0 || len(riskActions) > 0 {
		c.isDirty = true
	}
	c.mu.Unlock()

	if err := c.maybeSaveState(ctx, len(riskActions) > 0); err != nil {
		return err
	}

	return nil
}

func (c *GridCoordinator) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	if err := c.slotManager.OnOrderUpdate(ctx, update); err != nil {
		return err
	}

	c.mu.Lock()
	c.isDirty = true
	c.mu.Unlock()

	return c.maybeSaveState(ctx, false)
}

func (c *GridCoordinator) maybeSaveState(ctx context.Context, force bool) error {
	c.mu.Lock()

	// Reactive check: Only save if forced or (dirty + cooldown expired)
	shouldSave := force || (c.isDirty && time.Since(c.lastSaveTime) > 500*time.Millisecond)

	if !shouldSave {
		c.mu.Unlock()
		return nil
	}

	pVal := c.lastPrice
	c.mu.Unlock()
	return c.saveState(ctx, pVal)
}

func (c *GridCoordinator) GetRegimeMonitor() *monitor.RegimeMonitor {
	return c.regimeMonitor
}

func (c *GridCoordinator) SetStrategyID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.strategy.SetStrategyID(id)
}

func (c *GridCoordinator) saveState(ctx context.Context, lastPrice decimal.Decimal) error {
	c.mu.Lock()
	c.lastSaveTime = time.Now()
	c.isDirty = false
	isRiskTriggered := c.isRiskTriggered
	c.mu.Unlock()

	snap := c.slotManager.GetSnapshot()
	state := &pb.State{
		Slots:           snap.Slots,
		LastPrice:       pbu.FromGoDecimal(lastPrice),
		LastUpdateTime:  time.Now().UnixNano(),
		IsRiskTriggered: isRiskTriggered,
	}
	return c.store.SaveState(ctx, state)
}
