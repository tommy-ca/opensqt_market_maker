package gridengine

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/engine"
	"market_maker/internal/pb"
	"market_maker/pkg/concurrency"
	"market_maker/pkg/retry"
	"strings"
	"sync"
	"time"
)

// GridEngine is a lean orchestrator for grid trading
type GridEngine struct {
	coordinator *GridCoordinator
	exchange    core.IExchange
	executor    core.IOrderExecutor
	execPool    *concurrency.WorkerPool
	slotManager core.IPositionManager
	logger      core.ILogger
}

func NewGridEngine(
	exchanges map[string]core.IExchange,
	executor core.IOrderExecutor,
	monitor core.IRiskMonitor,
	store core.IStateStore,
	logger core.ILogger,
	execPool *concurrency.WorkerPool,
	slotMgr core.IPositionManager,
	cfg Config,
) engine.Engine {
	var exch core.IExchange
	for _, e := range exchanges {
		exch = e
		break
	}

	e := &GridEngine{
		exchange:    exch,
		executor:    executor,
		execPool:    execPool,
		slotManager: slotMgr,
		logger:      logger.WithField("component", "grid_engine"),
	}

	e.coordinator = NewGridCoordinator(cfg, slotMgr, monitor, store, e.logger, e)
	return e
}

func (e *GridEngine) Start(ctx context.Context) error {
	e.logger.Info("Starting Grid Engine")
	return e.coordinator.Start(ctx, e.exchange)
}

func (e *GridEngine) Stop() error {
	return nil
}

func (e *GridEngine) OnPriceUpdate(ctx context.Context, price *pb.PriceChange) error {
	return e.coordinator.OnPriceUpdate(ctx, price)
}

func (e *GridEngine) Execute(ctx context.Context, actions []*pb.OrderAction) {
	if len(actions) == 0 {
		return
	}

	policy := retry.RetryPolicy{
		MaxAttempts:    5,
		InitialBackoff: 200 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
	}

	isTransient := func(err error) bool {
		if err == nil {
			return false
		}
		msg := strings.ToLower(err.Error())
		return strings.Contains(msg, "rate limit") || strings.Contains(msg, "429") || strings.Contains(msg, "timeout")
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

			retryErr := retry.Do(ctx, policy, isTransient, func() error {
				if act.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE {
					order, err = e.executor.PlaceOrder(ctx, act.Request)
				} else {
					err = e.executor.BatchCancelOrders(ctx, act.Symbol, []int64{act.OrderId}, false)
				}
				return err
			})

			results[idx] = core.OrderActionResult{
				Action: act,
				Order:  order,
				Error:  retryErr,
			}
		}

		if e.execPool != nil {
			_ = e.execPool.Submit(task)
		} else {
			go task()
		}
	}

	wg.Wait()
	_ = e.slotManager.ApplyActionResults(results)
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
