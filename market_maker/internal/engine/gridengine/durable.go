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

// DBOSGridEngine is a durable orchestrator for grid trading using declarative reconciliation
type DBOSGridEngine struct {
	dbosCtx   dbos.DBOSContext
	exchanges map[string]core.IExchange
	executor  core.IOrderExecutor
	monitor   core.IRiskMonitor
	store     simple.Store
	logger    core.ILogger

	// Building Blocks
	strategy    *grid.GridStrategy
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
		Exchange:            cfg.Exchange,
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
		strategy:    grid.NewGridStrategy(strategyCfg),
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
	e.isRiskTriggered = isTriggeredNow

	// 3. Calculate Target State
	levels := e.getGridLevels()
	target, err := e.strategy.CalculateTargetState(ctx, pVal, e.anchorPrice, atr, volFactor, isTriggeredNow, false, levels)
	if err != nil {
		return err
	}

	// 4. Reconcile
	actions := e.reconcile(target)

	// 5. Execute Actions
	e.execute(ctx, actions)

	// 6. Save State
	e.saveState(ctx, pVal)

	return nil
}

func (e *DBOSGridEngine) getGridLevels() []grid.GridLevel {
	mgrSlots := e.slotManager.GetSlots()
	levels := make([]grid.GridLevel, 0, len(mgrSlots))
	for _, s := range mgrSlots {
		levels = append(levels, grid.GridLevel{
			Price:          pbu.ToGoDecimal(s.Price),
			PositionStatus: s.PositionStatus,
			PositionQty:    pbu.ToGoDecimal(s.PositionQty),
			SlotStatus:     s.SlotStatus,
			OrderSide:      s.OrderSide,
			OrderPrice:     pbu.ToGoDecimal(s.OrderPrice),
			OrderID:        s.OrderId,
		})
	}
	return levels
}

func (e *DBOSGridEngine) reconcile(target *core.TargetState) []*pb.OrderAction {
	var actions []*pb.OrderAction

	activeOrders := make(map[string]*core.InventorySlot)
	for _, s := range e.slotManager.GetSlots() {
		if s.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED && s.ClientOid != "" {
			activeOrders[s.ClientOid] = s
		}
	}

	desiredOids := make(map[string]bool)
	for _, to := range target.Orders {
		desiredOids[to.ClientOrderID] = true

		if _, exists := activeOrders[to.ClientOrderID]; !exists {
			actions = append(actions, &pb.OrderAction{
				Type:  pb.OrderActionType_ORDER_ACTION_TYPE_PLACE,
				Price: pbu.FromGoDecimal(to.Price),
				Request: &pb.PlaceOrderRequest{
					Symbol:        to.Symbol,
					Side:          e.mapSide(to.Side),
					Type:          pb.OrderType_ORDER_TYPE_LIMIT,
					Quantity:      pbu.FromGoDecimal(to.Quantity),
					Price:         pbu.FromGoDecimal(to.Price),
					ClientOrderId: to.ClientOrderID,
					ReduceOnly:    to.ReduceOnly,
					PostOnly:      to.PostOnly,
					TimeInForce:   pb.TimeInForce_TIME_IN_FORCE_GTC,
				},
			})
		}
	}

	for oid, s := range activeOrders {
		if !desiredOids[oid] {
			actions = append(actions, &pb.OrderAction{
				Type:    pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL,
				Symbol:  e.strategy.GetSymbol(),
				OrderId: s.OrderId,
				Price:   s.Price,
			})
		}
	}

	return actions
}

func (e *DBOSGridEngine) mapSide(side string) pb.OrderSide {
	if side == "BUY" {
		return pb.OrderSide_ORDER_SIDE_BUY
	}
	return pb.OrderSide_ORDER_SIDE_SELL
}

func (e *DBOSGridEngine) execute(ctx context.Context, actions []*pb.OrderAction) {
	if len(actions) == 0 {
		return
	}

	for _, act := range actions {
		_, err := e.dbosCtx.RunWorkflow(e.dbosCtx, e.ExecuteActionWorkflow, act)
		if err != nil {
			e.logger.Error("Failed to invoke durable workflow", "error", err)
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
