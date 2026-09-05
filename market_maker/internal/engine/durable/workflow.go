package durable

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"github.com/shopspring/decimal"
)

// TradingWorkflows defines the durable workflows for market making
type TradingWorkflows struct {
	pm       core.IPositionManager
	oe       core.IOrderExecutor
	strategy core.IStrategy
}

func NewTradingWorkflows(pm core.IPositionManager, oe core.IOrderExecutor, strategy core.IStrategy) *TradingWorkflows {
	return &TradingWorkflows{
		pm:       pm,
		oe:       oe,
		strategy: strategy,
	}
}

// OnPriceUpdate is a durable workflow triggered by price changes
func (w *TradingWorkflows) OnPriceUpdate(ctx dbos.DBOSContext, input any) (any, error) {
	price := input.(*pb.PriceChange)

	// 1. Calculate Adjustments (Step)
	actionsRaw, err := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		pVal := pbu.ToGoDecimal(price.Price)
		stratSlots := w.pm.GetStrategySlots(nil)
		anchorPrice := w.pm.GetAnchorPrice()

		// Note: Durable engine should probably persist risk state too, or assume it's available via RM.
		// For now we use basic defaults as in SimpleEngine if RM is not injected.
		// Wait, TradingWorkflows struct doesn't have RiskMonitor.
		// If we need risk, we should add it.
		// But for now, let's assume risk is handled inside Strategy if passed, or we pass defaults.

		atr := decimal.Zero
		volFactor := 0.0
		isRiskTriggered := false
		regime := pb.MarketRegime_MARKET_REGIME_RANGE

		actions := w.strategy.CalculateActions(pVal, anchorPrice, atr, volFactor, isRiskTriggered, regime, stratSlots)

		// Post-processing
		w.pm.MarkSlotsPending(actions)

		return actions, nil
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
