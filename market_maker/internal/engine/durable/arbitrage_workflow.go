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
// Input: *ArbitrageEntryRequest
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

	// Step 1: Place Spot Buy
	spotOrderRaw, err := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		return spotEx.PlaceOrder(ctx, req.SpotOrder)
	})
	if err != nil {
		// Failed to place spot order, abort (nothing to unwind)
		return nil, fmt.Errorf("failed to place spot order: %w", err)
	}
	spotOrder := spotOrderRaw.(*pb.Order)

	// Step 2: Place Perp Sell
	// Align Perp quantity with actual Spot execution to maintain delta neutrality
	perpReq := req.PerpOrder
	if !pbu.ToGoDecimal(spotOrder.ExecutedQty).IsZero() {
		perpReq.Quantity = spotOrder.ExecutedQty
	}

	_, err = ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		return perpEx.PlaceOrder(ctx, perpReq)
	})

	if err != nil {
		// Perp leg failed! We are now net long. Must unwind spot.
		// Step 3: Compensation (Sell Spot)
		// We execute this as a step to ensure it happens.
		// Ideally this should be a "compensation" logic or separate workflow,
		// but simple compensation step works for now.

		// Use ExecutedQty from spot order for accurate unwind, fallback to requested qty
		unwindQty := spotOrder.ExecutedQty
		if pbu.ToGoDecimal(unwindQty).IsZero() {
			unwindQty = req.SpotOrder.Quantity
		}

		// Compensation: Unwind spot leg
		unwindSide := pb.OrderSide_ORDER_SIDE_SELL
		if req.SpotOrder.Side == pb.OrderSide_ORDER_SIDE_SELL {
			unwindSide = pb.OrderSide_ORDER_SIDE_BUY
		}

		_, unwindErr := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
			unwindReq := &pb.PlaceOrderRequest{
				Symbol:        spotOrder.Symbol,
				Side:          unwindSide,
				Type:          pb.OrderType_ORDER_TYPE_MARKET,
				Quantity:      unwindQty,
				ClientOrderId: fmt.Sprintf("unwind_%s", spotOrder.ClientOrderId),
			}
			return spotEx.PlaceOrder(ctx, unwindReq)
		})

		if unwindErr != nil {
			// Critical failure: Stuck with open leg.
			// Log critical error / Alert
			return nil, fmt.Errorf("CRITICAL: Failed to unwind spot position after perp failure: %v (Original error: %v)", unwindErr, err)
		}

		return nil, fmt.Errorf("perp leg failed, unwound spot position: %w", err)
	}

	return nil, nil
}

// ExecuteSpotPerpExit executes a delta-neutral exit (Spot Sell + Perp Buy)
// Input: *ArbitrageExitRequest
func (w *ArbitrageWorkflows) ExecuteSpotPerpExit(ctx dbos.DBOSContext, input any) (any, error) {
	req := input.(*pb.ArbitrageExitRequest)

	spotEx, ok := w.exchanges[req.SpotExchange]
	if !ok {
		return nil, fmt.Errorf("spot exchange %s not found", req.SpotExchange)
	}
	perpEx, ok := w.exchanges[req.PerpExchange]
	if !ok {
		return nil, fmt.Errorf("perp exchange %s not found", req.PerpExchange)
	}

	// Exit Logic: Close both legs. Order doesn't matter as much as Entry, but usually safer to close Perp (Leverage) first?
	// Or Spot first to secure realized PnL?
	// Let's close Perp first (Buy to Cover) then Spot (Sell).
	// Ideally we do this concurrently, but DBOS steps are sequential?
	// Actually we can launch goroutines inside a step, but standard DBOS pattern is sequential steps.
	// Sequential is safer for state tracking.

	// Step 1: Close Perp (Buy)
	perpOrderRaw, err := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		return perpEx.PlaceOrder(ctx, req.PerpOrder)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to close perp position: %w", err)
	}
	perpOrder := perpOrderRaw.(*pb.Order)

	// Step 2: Close Spot (Sell)
	// Align Spot quantity with actual Perp execution
	spotReq := req.SpotOrder
	if !pbu.ToGoDecimal(perpOrder.ExecutedQty).IsZero() {
		spotReq.Quantity = perpOrder.ExecutedQty
	}

	_, err = ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		return spotEx.PlaceOrder(ctx, spotReq)
	})

	if err != nil {
		// Spot sell failed! We closed the hedge but still hold the asset.
		// We are now net Long delta (holding Spot).
		// Risk: Price drops.
		// Remediation: Retry Spot Sell? Or Re-open Perp?
		// Usually we want to exit, so we should retry Spot Sell.
		// Since we can't easily retry indefinitely here, we return error and rely on manual intervention or upper layer retry.
		// Or we can try one compensation: Re-open Perp (Short) to re-hedge?
		// No, if we are exiting, we likely want out. Retrying spot sell is better.
		// For now, fail and log.

		return nil, fmt.Errorf("CRITICAL: Failed to close spot position after closing perp: %w", err)
	}

	return nil, nil
}
