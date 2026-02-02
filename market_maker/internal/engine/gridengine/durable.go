package gridengine

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/engine"
	"market_maker/internal/engine/simple"
	"market_maker/internal/pb"
	"market_maker/internal/trading/grid"
	"market_maker/pkg/pbu"
	"sync"
	"time"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"github.com/shopspring/decimal"
)

// DBOSGridEngine is a durable orchestrator for grid trading
type DBOSGridEngine struct {
	dbosCtx   dbos.DBOSContext
	exchanges map[string]core.IExchange
	executor  core.IOrderExecutor
	monitor   core.IRiskMonitor
	store     simple.Store
	logger    core.ILogger

	// Building Blocks
	strategy    *grid.Strategy
	slotManager core.IPositionManager

	// State
	anchorPrice decimal.Decimal
	mu          sync.Mutex

	// Status tracking
	isRiskTriggered bool
}

func NewDBOSGridEngine(
	dbosCtx dbos.DBOSContext,
	exchanges map[string]core.IExchange,
	executor core.IOrderExecutor,
	monitor core.IRiskMonitor,
	store simple.Store,
	logger core.ILogger,
	slotMgr core.IPositionManager,
	cfg Config,
) engine.Engine {
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

	return &DBOSGridEngine{
		dbosCtx:     dbosCtx,
		exchanges:   exchanges,
		executor:    executor,
		monitor:     monitor,
		store:       store,
		logger:      logger.WithField("component", "dbos_grid_engine"),
		strategy:    grid.NewStrategy(strategyCfg),
		slotManager: slotMgr,
	}
}

func (e *DBOSGridEngine) Start(ctx context.Context) error {
	e.logger.Info("Starting DBOS Grid Engine")

	// Restore State
	state, err := e.store.LoadState(ctx)
	if err == nil && state != nil {
		_ = e.slotManager.RestoreState(state.Slots)
		e.anchorPrice = pbu.ToGoDecimal(state.LastPrice)
		e.logger.Info("State restored", "slots", len(state.Slots), "anchor", e.anchorPrice)
	}

	return e.dbosCtx.Launch()
}

func (e *DBOSGridEngine) Stop() error {
	e.dbosCtx.Shutdown(30 * 1000 * 1000 * 1000)
	return nil
}

func (e *DBOSGridEngine) OnPriceUpdate(ctx context.Context, price *pb.PriceChange) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	pVal := pbu.ToGoDecimal(price.Price)

	if e.anchorPrice.IsZero() {
		e.anchorPrice = pVal
	}

	// 1. Get ATR and Risk Status
	atr := decimal.Zero
	volFactor := 0.0
	isTriggeredNow := false
	if e.monitor != nil {
		atr = e.monitor.GetATR(price.Symbol)
		volFactor = e.monitor.GetVolatilityFactor(price.Symbol)
		isTriggeredNow = e.monitor.IsTriggered()
	}

	// 2. Handle Risk Transition
	if isTriggeredNow && !e.isRiskTriggered {
		e.logger.Warn("Risk Triggered! Canceling all Buy orders")
		actions, _ := e.slotManager.CancelAllBuyOrders(ctx)
		e.execute(ctx, actions)
	}
	e.isRiskTriggered = isTriggeredNow

	// 3. Calculate Actions
	mgrSlots := e.slotManager.GetSlots()
	stratSlots := make([]grid.Slot, 0, len(mgrSlots))
	for _, s := range mgrSlots {
		stratSlots = append(stratSlots, grid.Slot{
			Price:          pbu.ToGoDecimal(s.Price),
			PositionStatus: s.PositionStatus,
			PositionQty:    pbu.ToGoDecimal(s.PositionQty),
			SlotStatus:     s.SlotStatus,
			OrderSide:      s.OrderSide,
			OrderPrice:     pbu.ToGoDecimal(s.OrderPrice),
		})
	}

	actions := e.strategy.CalculateActions(pVal, e.anchorPrice, atr, volFactor, isTriggeredNow, stratSlots)

	// 4. Execute Actions
	e.execute(ctx, actions)

	// 5. Save State
	e.saveState(ctx, pVal)

	return nil
}

func (e *DBOSGridEngine) execute(ctx context.Context, actions []*pb.OrderAction) {
	if len(actions) == 0 {
		return
	}

	// For each action, start a durable workflow
	for _, act := range actions {
		_, err := e.dbosCtx.RunWorkflow(e.dbosCtx, e.ExecuteActionWorkflow, act)
		if err != nil {
			e.logger.Error("Failed to invoke durable workflow", "error", err)
			// Fallback: apply error immediately if workflow couldn't start
			_ = e.slotManager.ApplyActionResults([]core.OrderActionResult{{Action: act, Error: err}})
		}
	}
}

// ExecuteActionWorkflow is a durable workflow to execute a single order action
func (e *DBOSGridEngine) ExecuteActionWorkflow(ctx dbos.DBOSContext, input any) (any, error) {
	action := input.(*pb.OrderAction)
	resultRaw, err := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		res := core.OrderActionResult{Action: action}
		switch action.Type {
		case pb.OrderActionType_ORDER_ACTION_TYPE_PLACE:
			order, err := e.executor.PlaceOrder(ctx, action.Request)
			res.Order = order
			res.Error = err
		case pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL:
			err := e.executor.BatchCancelOrders(ctx, action.Symbol, []int64{action.OrderId}, false)
			res.Error = err
		}
		return res, nil
	})

	if err != nil {
		return nil, err
	}

	result := resultRaw.(core.OrderActionResult)

	// Apply result in a step to ensure state update is also durable
	_, err = ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		return nil, e.slotManager.ApplyActionResults([]core.OrderActionResult{result})
	})

	return nil, err
}

func (e *DBOSGridEngine) saveState(ctx context.Context, lastPrice decimal.Decimal) {
	snap := e.slotManager.GetSnapshot()
	state := &pb.State{
		Slots:          snap.Slots,
		LastPrice:      pbu.FromGoDecimal(lastPrice),
		LastUpdateTime: time.Now().UnixNano(),
	}
	_ = e.store.SaveState(ctx, state)
}

func (e *DBOSGridEngine) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	return e.slotManager.OnOrderUpdate(ctx, update)
}

func (e *DBOSGridEngine) OnFundingUpdate(ctx context.Context, update *pb.FundingUpdate) error {
	return nil
}

func (e *DBOSGridEngine) OnPositionUpdate(ctx context.Context, position *pb.Position) error {
	return nil
}

func (e *DBOSGridEngine) OnAccountUpdate(ctx context.Context, account *pb.Account) error {
	return nil
}
