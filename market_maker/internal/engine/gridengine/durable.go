package gridengine

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/engine"
	"market_maker/internal/pb"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
)

// DBOSGridEngine is a durable orchestrator for grid trading
type DBOSGridEngine struct {
	coordinator *GridCoordinator
	dbosCtx     dbos.DBOSContext
	exchange    core.IExchange
	executor    core.IOrderExecutor
	slotManager core.IPositionManager
	logger      core.ILogger
}

func NewDBOSGridEngine(
	dbosCtx dbos.DBOSContext,
	exchanges map[string]core.IExchange,
	executor core.IOrderExecutor,
	monitor core.IRiskMonitor,
	store core.IStateStore,
	logger core.ILogger,
	slotMgr core.IPositionManager,
	cfg Config,
) engine.Engine {
	var exch core.IExchange
	for _, e := range exchanges {
		exch = e
		break
	}

	e := &DBOSGridEngine{
		dbosCtx:     dbosCtx,
		exchange:    exch,
		executor:    executor,
		slotManager: slotMgr,
		logger:      logger.WithField("component", "dbos_grid_engine"),
	}

	e.coordinator = NewGridCoordinator(cfg, slotMgr, monitor, store, e.logger, e)
	return e
}

func (e *DBOSGridEngine) Start(ctx context.Context) error {
	e.logger.Info("Starting DBOS Grid Engine")
	if err := e.coordinator.Start(ctx, e.exchange); err != nil {
		return err
	}
	return e.dbosCtx.Launch()
}

func (e *DBOSGridEngine) Stop() error {
	e.dbosCtx.Shutdown(30 * 1000 * 1000 * 1000)
	return nil
}

func (e *DBOSGridEngine) OnPriceUpdate(ctx context.Context, price *pb.PriceChange) error {
	return e.coordinator.OnPriceUpdate(ctx, price)
}

func (e *DBOSGridEngine) Execute(ctx context.Context, actions []*pb.OrderAction) {
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
