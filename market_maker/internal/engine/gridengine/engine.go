package gridengine

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/engine"
	"market_maker/internal/engine/simple"
	"market_maker/internal/pb"
	"market_maker/internal/trading/grid"
	"market_maker/pkg/concurrency"
	"market_maker/pkg/pbu"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// GridEngine is a lean orchestrator for grid trading
type GridEngine struct {
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

	// Concurrency
	execPool *concurrency.WorkerPool

	// Status tracking
	isRiskTriggered bool
}

func NewGridEngine(
	exchanges map[string]core.IExchange,
	executor core.IOrderExecutor,
	monitor core.IRiskMonitor,
	store simple.Store,
	logger core.ILogger,
	execPool *concurrency.WorkerPool,
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

	return &GridEngine{
		exchanges:   exchanges,
		executor:    executor,
		monitor:     monitor,
		store:       store,
		logger:      logger.WithField("component", "grid_engine"),
		strategy:    grid.NewStrategy(strategyCfg),
		slotManager: slotMgr,
		execPool:    execPool,
	}
}

func (e *GridEngine) Start(ctx context.Context) error {
	e.logger.Info("Starting Grid Engine")

	// Restore State
	state, err := e.store.LoadState(ctx)
	if err == nil && state != nil {
		_ = e.slotManager.RestoreState(state.Slots)
		e.anchorPrice = pbu.ToGoDecimal(state.LastPrice)
		e.logger.Info("State restored", "slots", len(state.Slots), "anchor", e.anchorPrice)
	}

	return nil
}

func (e *GridEngine) Stop() error {
	return nil
}

func (e *GridEngine) OnPriceUpdate(ctx context.Context, price *pb.PriceChange) error {
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

	// 3. Calculate Actions
	slots := e.getSlots()
	actions := e.strategy.CalculateActions(pVal, e.anchorPrice, atr, volFactor, isTriggeredNow, slots)

	// 4. Execute Actions
	e.execute(ctx, actions)

	// 5. Save State
	e.saveState(ctx, pVal)

	return nil
}

func (e *GridEngine) getSlots() []grid.Slot {
	mgrSlots := e.slotManager.GetSlots()
	slots := make([]grid.Slot, 0, len(mgrSlots))
	for _, s := range mgrSlots {
		slots = append(slots, grid.Slot{
			Price:          pbu.ToGoDecimal(s.Price),
			PositionStatus: s.PositionStatus,
			PositionQty:    pbu.ToGoDecimal(s.PositionQty),
			SlotStatus:     s.SlotStatus,
			OrderSide:      s.OrderSide,
			OrderPrice:     pbu.ToGoDecimal(s.OrderPrice),
		})
	}
	return slots
}

func (e *GridEngine) execute(ctx context.Context, actions []*pb.OrderAction) {
	if len(actions) == 0 {
		return
	}

	results := make([]core.OrderActionResult, len(actions))
	wg := sync.WaitGroup{}
	wg.Add(len(actions))

	for i, action := range actions {
		idx := i
		act := action

		task := func() {
			defer wg.Done()
			var order *pb.Order
			var err error

			if act.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE {
				order, err = e.executor.PlaceOrder(ctx, act.Request)
			} else {
				err = e.executor.BatchCancelOrders(ctx, act.Symbol, []int64{act.OrderId}, false)
			}

			results[idx] = core.OrderActionResult{
				Action: act,
				Order:  order,
				Error:  err,
			}
		}

		if e.execPool != nil {
			_ = e.execPool.Submit(task)
		} else {
			go task()
		}
	}

	// In SimpleEngine we should wait for results to apply them atomically
	// This is slightly blocking but ensures state consistency in procedural loop
	wg.Wait()
	_ = e.slotManager.ApplyActionResults(results)
}

func (e *GridEngine) saveState(ctx context.Context, lastPrice decimal.Decimal) {
	snap := e.slotManager.GetSnapshot()
	state := &pb.State{
		Slots:          snap.Slots,
		LastPrice:      pbu.FromGoDecimal(lastPrice),
		LastUpdateTime: time.Now().UnixNano(),
	}
	_ = e.store.SaveState(ctx, state)
}

func (e *GridEngine) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	return e.slotManager.OnOrderUpdate(ctx, update)
}

func (e *GridEngine) OnFundingUpdate(ctx context.Context, update *pb.FundingUpdate) error {
	return nil
}

func (e *GridEngine) OnPositionUpdate(ctx context.Context, position *pb.Position) error {
	return nil
}

func (e *GridEngine) OnAccountUpdate(ctx context.Context, account *pb.Account) error {
	return nil
}
