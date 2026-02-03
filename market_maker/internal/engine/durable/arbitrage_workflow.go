package durable

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
)

// ArbitrageWorkflows defines durable workflows for arbitrage strategies
type ArbitrageWorkflows struct {
	exchanges map[string]core.IExchange
}

func NewArbitrageWorkflows(exchanges map[string]core.IExchange) *ArbitrageWorkflows {
	return &ArbitrageWorkflows{
		exchanges: exchanges,
	}
}

// ExecuteSpotPerpEntry executes a delta-neutral entry (Spot Buy + Perp Sell)
// Hardened with IOC and immediate fill scaling.
func (w *ArbitrageWorkflows) ExecuteSpotPerpEntry(ctx dbos.DBOSContext, input any) (any, error) {
	req := input.(*pb.ArbitrageEntryRequest)

	spotEx, ok := w.exchanges[req.SpotExchange]
	if !ok {
		return nil, fmt.Errorf("spot exchange %s not found", req.SpotExchange)
	}
	perpEx, ok := w.exchanges[req.PerpExchange]
	if !ok {
		return nil, fmt.Errorf("perp exchange %s not found", req.PerpExchange)
	}

	// Step 1: Place Spot Order (Enforce IOC/FOK for atomic fill knowledge)
	req.SpotOrder.TimeInForce = pb.TimeInForce_TIME_IN_FORCE_IOC

	spotOrderRaw, err := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		return spotEx.PlaceOrder(ctx, req.SpotOrder)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to place spot order: %w", err)
	}
	spotOrder := spotOrderRaw.(*pb.Order)

	// Step 2: Scale Perp leg based on actual Spot execution
	spotQty := pbu.ToGoDecimal(spotOrder.ExecutedQty)
	if spotQty.IsZero() {
		return nil, fmt.Errorf("spot order not filled (IOC)")
	}

	perpReq := req.PerpOrder
	perpReq.Quantity = spotOrder.ExecutedQty // Correctly scaled quantity

	_, err = ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		return perpEx.PlaceOrder(ctx, perpReq)
	})

	if err != nil {
		// Step 3: Compensation (Unwind partial spot)
		_, unwindErr := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
			unwindSide := pb.OrderSide_ORDER_SIDE_SELL
			if req.SpotOrder.Side == pb.OrderSide_ORDER_SIDE_SELL {
				unwindSide = pb.OrderSide_ORDER_SIDE_BUY
			}
			unwindReq := &pb.PlaceOrderRequest{
				Symbol:        spotOrder.Symbol,
				Side:          unwindSide,
				Type:          pb.OrderType_ORDER_TYPE_MARKET,
				Quantity:      spotOrder.ExecutedQty,
				ClientOrderId: fmt.Sprintf("unwind_%s", spotOrder.ClientOrderId),
			}
			return spotEx.PlaceOrder(ctx, unwindReq)
		})

		if unwindErr != nil {
			return nil, fmt.Errorf("CRITICAL: Failed to unwind spot position: %v (Original: %v)", unwindErr, err)
		}
		return nil, fmt.Errorf("perp leg failed, unwound spot: %w", err)
	}

	return nil, nil
}

// ExecuteSpotPerpExit executes a delta-neutral exit (Spot Sell + Perp Buy)
// Hardened with concurrent closures via sub-workflows.
func (w *ArbitrageWorkflows) ExecuteSpotPerpExit(ctx dbos.DBOSContext, input any) (any, error) {
	req := input.(*pb.ArbitrageExitRequest)

	// To achieve atomic parallel exits while maintaining durability,
	// we launch two independent sub-workflows.

	h1, err := ctx.RunWorkflow(ctx, w.ExecuteSingleLegExit, &SingleLegReq{
		Exchange: req.SpotExchange,
		Order:    req.SpotOrder,
	})
	if err != nil {
		return nil, err
	}

	h2, err := ctx.RunWorkflow(ctx, w.ExecuteSingleLegExit, &SingleLegReq{
		Exchange: req.PerpExchange,
		Order:    req.PerpOrder,
	})
	if err != nil {
		return nil, err
	}

	// Wait for both to finish
	_, err1 := h1.GetResult()
	_, err2 := h2.GetResult()

	if err1 != nil || err2 != nil {
		return nil, fmt.Errorf("one or both exit legs failed: spot=%v, perp=%v", err1, err2)
	}

	return nil, nil
}

type SingleLegReq struct {
	Exchange string
	Order    *pb.PlaceOrderRequest
}

func (w *ArbitrageWorkflows) ExecuteSingleLegExit(ctx dbos.DBOSContext, input any) (any, error) {
	req := input.(*SingleLegReq)
	ex, ok := w.exchanges[req.Exchange]
	if !ok {
		return nil, fmt.Errorf("exchange %s not found", req.Exchange)
	}

	return ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		return ex.PlaceOrder(ctx, req.Order)
	})
}
