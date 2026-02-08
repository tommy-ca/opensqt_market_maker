package gridengine

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/engine/simple"
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
	store       simple.Store
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
	store simple.Store,
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
		c.logger.Info("Local state restored", "slots", len(state.Slots), "anchor", c.anchorPrice)
	}

	// 2. Exchange Reconciliation (The Reality Check)
	if exch != nil {
		c.logger.Info("Reconciling with exchange...")
		openOrders, err := exch.GetOpenOrders(ctx, c.symbol, false)
		if err != nil {
			return fmt.Errorf("failed to fetch open orders during boot: %w", err)
		}

		c.slotManager.SyncOrders(openOrders)
		c.logger.Info("Exchange reconciliation complete", "open_orders", len(openOrders))

		// 3. Position Sync (Inventory Reconciliation)
		positions, err := exch.GetPositions(ctx, c.symbol)
		if err == nil {
			totalPos := decimal.Zero
			for _, p := range positions {
				totalPos = totalPos.Add(pbu.ToGoDecimal(p.Size))
			}
			c.logger.Info("Current exchange position", "size", totalPos)
			c.slotManager.RestoreFromExchangePosition(totalPos)
		} else {
			c.logger.Error("Failed to fetch positions during boot", "error", err)
		}
	}

	return nil
}

func (c *GridCoordinator) OnPriceUpdate(ctx context.Context, price *pb.PriceChange) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	pVal := pbu.ToGoDecimal(price.Price)

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
		actions, _ := c.slotManager.CancelAllBuyOrders(ctx)
		c.executor.Execute(ctx, actions)
	}
	c.isRiskTriggered = isTriggeredNow

	// 3. Calculate Actions
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

	actions := c.strategy.CalculateActions(pVal, c.anchorPrice, atr, volFactor, isTriggeredNow, stratSlots)

	// 4. Execute Actions
	c.executor.Execute(ctx, actions)

	// 5. Save State
	c.saveState(ctx, pVal)

	return nil
}

func (c *GridCoordinator) saveState(ctx context.Context, lastPrice decimal.Decimal) {
	snap := c.slotManager.GetSnapshot()
	state := &pb.State{
		Slots:          snap.Slots,
		LastPrice:      pbu.FromGoDecimal(lastPrice),
		LastUpdateTime: time.Now().UnixNano(),
	}
	_ = c.store.SaveState(ctx, state)
}
