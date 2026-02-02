package durable

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
)

// TradingWorkflows defines the durable workflows for market making
type TradingWorkflows struct {
	pm core.IPositionManager
	oe core.IOrderExecutor
}

func NewTradingWorkflows(pm core.IPositionManager, oe core.IOrderExecutor) *TradingWorkflows {
	return &TradingWorkflows{
		pm: pm,
		oe: oe,
	}
}

// OnPriceUpdate is a durable workflow triggered by price changes
func (w *TradingWorkflows) OnPriceUpdate(ctx dbos.DBOSContext, input any) (any, error) {
	price := input.(*pb.PriceChange)

	// 1. Calculate Adjustments (Step)
	actionsRaw, err := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		return w.pm.CalculateAdjustments(ctx, pbu.ToGoDecimal(price.Price))
	})
	if err != nil {
		return nil, err
	}
	actions := actionsRaw.([]*pb.OrderAction)

	if len(actions) == 0 {
		return nil, nil
	}

	// 2. Execute Actions (Steps)
	results := make([]core.OrderActionResult, len(actions))
	for i, action := range actions {
		resultRaw, err := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
			res := core.OrderActionResult{Action: action}
			switch action.Type {
			case pb.OrderActionType_ORDER_ACTION_TYPE_PLACE:
				order, err := w.oe.PlaceOrder(ctx, action.Request)
				res.Order = order
				res.Error = err
			case pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL:
				err := w.oe.BatchCancelOrders(ctx, action.Symbol, []int64{action.OrderId}, false)
				res.Error = err
			}
			return res, nil
		})
		if err != nil {
			results[i] = core.OrderActionResult{Action: action, Error: err}
		} else {
			results[i] = resultRaw.(core.OrderActionResult)
		}
	}

	// 3. Apply Results (Step)
	_, err = ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		return nil, w.pm.ApplyActionResults(results)
	})

	return nil, err
}

// OnOrderUpdate is a durable workflow triggered by order updates
func (w *TradingWorkflows) OnOrderUpdate(ctx dbos.DBOSContext, input any) (any, error) {
	update := input.(*pb.OrderUpdate)

	_, err := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		return nil, w.pm.OnOrderUpdate(ctx, update)
	})

	return nil, err
}
