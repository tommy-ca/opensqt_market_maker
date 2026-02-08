package gridengine

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/internal/trading/grid"
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
	symbol      string
	strategy    *grid.Strategy
	slotManager core.IPositionManager
	monitor     core.IRiskMonitor
	store       core.IStateStore
	logger      core.ILogger
	executor    IGridExecutor

	anchorPrice     decimal.Decimal
	isRiskTriggered bool
	mu              sync.Mutex
}

func NewGridCoordinator(
	cfg Config,
	slotMgr core.IPositionManager,
	monitor core.IRiskMonitor,
	store core.IStateStore,
	logger core.ILogger,
	executor IGridExecutor,
) *GridCoordinator {
	strategyCfg := grid.StrategyConfig{
		Symbol:              cfg.Symbol,
		PriceInterval:       cfg.PriceInterval,
		OrderQuantity:       cfg.OrderQuantity,
		MinOrderValue:       cfg.MinOrderValue,
		BuyWindowSize:       cfg.BuyWindowSize,
		SellWindowSize:      cfg.SellWindowSize,
		PriceDecimals:       cfg.PriceDecimals,
		QtyDecimals:         cfg.QtyDecimals,
		IsNeutral:           cfg.IsNeutral,
		VolatilityScale:     cfg.VolatilityScale,
		InventorySkewFactor: cfg.InventorySkewFactor,
	}

	return &GridCoordinator{
		symbol:      cfg.Symbol,
		strategy:    grid.NewStrategy(strategyCfg),
		slotManager: slotMgr,
		monitor:     monitor,
		store:       store,
		logger:      logger,
		executor:    executor,
	}
}

func (c *GridCoordinator) Start(ctx context.Context, exch core.IExchange) error {
	c.logger.Info("Starting Grid Coordinator", "symbol", c.symbol)

	// 1. Restore Local State (Warm Boot)
	state, err := c.store.LoadState(ctx)
	if err == nil && state != nil {
		_ = c.slotManager.RestoreState(state.Slots)
		c.anchorPrice = pbu.ToGoDecimal(state.LastPrice)
		c.isRiskTriggered = state.IsRiskTriggered
		c.logger.Info("Local state restored", "slots", len(state.Slots), "anchor", c.anchorPrice, "risk_triggered", c.isRiskTriggered)
	}

	// 2. Exchange Reconciliation (The Reality Check)
	if exch != nil {
		c.logger.Info("Reconciling with exchange...")

		var openOrders []*pb.Order
		var positions []*pb.Position
		var ordersErr, posErr error
		var wg sync.WaitGroup

		wg.Add(2)
		go func() {
			defer wg.Done()
			openOrders, ordersErr = exch.GetOpenOrders(ctx, c.symbol, false)
		}()

		go func() {
			defer wg.Done()
			positions, posErr = exch.GetPositions(ctx, c.symbol)
		}()

		wg.Wait()

		if ordersErr != nil {
			return fmt.Errorf("failed to fetch open orders during boot: %w", ordersErr)
		}

		c.slotManager.SyncOrders(openOrders)
		c.logger.Info("Exchange reconciliation complete", "open_orders", len(openOrders))

		if posErr == nil {
			totalPos := decimal.Zero
			for _, p := range positions {
				totalPos = totalPos.Add(pbu.ToGoDecimal(p.Size))
			}
			c.logger.Info("Current exchange position", "size", totalPos)
			c.slotManager.RestoreFromExchangePosition(totalPos)
		} else {
			c.logger.Error("Failed to fetch positions during boot", "error", posErr)
		}
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
		riskActions, _ = c.slotManager.CancelAllBuyOrders(ctx)
	}
	c.isRiskTriggered = isTriggeredNow

	// 3. Calculate Strategy Actions
	mgrSlots := c.slotManager.GetSlots()
	stratSlots := make([]grid.Slot, 0, len(mgrSlots))
	for _, s := range mgrSlots {
		stratSlots = append(stratSlots, grid.Slot{
			Price:          pbu.ToGoDecimal(s.Price),
			PositionStatus: s.PositionStatus,
			PositionQty:    pbu.ToGoDecimal(s.PositionQty),
			SlotStatus:     s.SlotStatus,
			OrderSide:      s.OrderSide,
			OrderPrice:     pbu.ToGoDecimal(s.OrderPrice),
			OrderId:        s.OrderId,
		})
	}

	strategyActions = c.strategy.CalculateActions(pVal, c.anchorPrice, atr, volFactor, isTriggeredNow, stratSlots)
	c.mu.Unlock()

	// Phase 2: Execution (Unlocked - prevents blocking hot path)
	if len(riskActions) > 0 {
		c.executor.Execute(ctx, riskActions)
	}
	if len(strategyActions) > 0 {
		c.executor.Execute(ctx, strategyActions)
	}

	// Phase 3: Persistence
	c.saveState(ctx, pVal)

	return nil
}

func (c *GridCoordinator) saveState(ctx context.Context, lastPrice decimal.Decimal) {
	snap := c.slotManager.GetSnapshot()
	state := &pb.State{
		Slots:           snap.Slots,
		LastPrice:       pbu.FromGoDecimal(lastPrice),
		LastUpdateTime:  time.Now().UnixNano(),
		IsRiskTriggered: c.isRiskTriggered,
	}
	_ = c.store.SaveState(ctx, state)
}
