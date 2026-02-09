package gridengine

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/internal/trading/grid"
	"market_maker/internal/trading/monitor"
	"market_maker/pkg/pbu"
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
	isRiskTriggered bool
	lastSaveTime    time.Time
	stratSlots      []grid.Slot
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
	for _, e := range deps.Exchanges {
		exch = e
		break
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
		lastSaveTime:  time.Time{},
	}
}

func (c *GridCoordinator) Start(ctx context.Context) error {
	c.logger.Info("Starting Grid Coordinator", "symbol", c.symbol)

	// 1. Restore Local State (Warm Boot)
	state, err := c.store.LoadState(ctx)
	if err != nil {
		return fmt.Errorf("failed to load state from store: %w", err)
	}

	if state != nil {
		if err := c.slotManager.RestoreState(state.Slots); err != nil {
			return fmt.Errorf("failed to restore state in slot manager: %w", err)
		}
		c.anchorPrice = pbu.ToGoDecimal(state.LastPrice)
		c.isRiskTriggered = state.IsRiskTriggered
		c.logger.Info("Local state restored", "slots", len(state.Slots), "anchor", c.anchorPrice, "risk_triggered", c.isRiskTriggered)
	}

	// 2. Exchange Reconciliation (The Reality Check)
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

	// 3. Calculate Strategy Actions
	mgrSlots := c.slotManager.GetSlots()
	if cap(c.stratSlots) < len(mgrSlots) {
		c.stratSlots = make([]grid.Slot, 0, len(mgrSlots))
	}
	c.stratSlots = c.stratSlots[:0]

	for _, s := range mgrSlots {
		s.Mu.RLock()
		c.stratSlots = append(c.stratSlots, grid.Slot{
			Price:          pbu.ToGoDecimal(s.Price),
			PositionStatus: s.PositionStatus,
			PositionQty:    pbu.ToGoDecimal(s.PositionQty),
			SlotStatus:     s.SlotStatus,
			OrderSide:      s.OrderSide,
			OrderPrice:     pbu.ToGoDecimal(s.OrderPrice),
			OrderId:        s.OrderId,
		})
		s.Mu.RUnlock()
	}

	// 4. Get Current Regime
	currentRegime := pb.MarketRegime_MARKET_REGIME_RANGE
	if c.regimeMonitor != nil {
		currentRegime = c.regimeMonitor.GetRegime()
	}

	strategyActions = c.strategy.CalculateActions(pVal, c.anchorPrice, atr, volFactor, isTriggeredNow, currentRegime, c.stratSlots)
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
	hasActivity := len(strategyActions) > 0 || len(riskActions) > 0
	isHeartbeat := time.Since(c.lastSaveTime) > 30*time.Second

	// Throttling: only save if enough time has passed (500ms), unless it's a heartbeat or risk trigger
	shouldSave := isHeartbeat || len(riskActions) > 0 || (hasActivity && time.Since(c.lastSaveTime) > 500*time.Millisecond)
	c.mu.Unlock()

	if shouldSave {
		if err := c.saveState(ctx, pVal); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}
	}

	return nil
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
