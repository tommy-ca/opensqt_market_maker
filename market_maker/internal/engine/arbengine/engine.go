package arbengine

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/engine"
	"market_maker/internal/pb"
	"market_maker/internal/trading/arbitrage"
	"market_maker/internal/trading/execution"
	"market_maker/pkg/pbu"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"market_maker/pkg/telemetry"
)

type ArbitrageEngine struct {
	exchanges      map[string]core.IExchange
	monitor        core.IRiskMonitor
	fundingMonitor core.IFundingMonitor
	logger         core.ILogger

	// Building Blocks
	strategy         *arbitrage.Strategy
	legManager       *arbitrage.LegManager
	executor         *execution.SequenceExecutor
	parallelExecutor *execution.ParallelExecutor

	// Config
	symbol        string
	spotExchange  string
	perpExchange  string
	orderQuantity decimal.Decimal
	stalenessTTL  time.Duration

	// State
	mu                  sync.Mutex
	isExecuting         bool
	lastNextFundingTime int64
	toxicBasisCount     int
	lastSpotPrice       decimal.Decimal
	lastPerpPrice       decimal.Decimal
}

func NewArbitrageEngine(
	exchanges map[string]core.IExchange,
	monitor core.IRiskMonitor,
	fundingMonitor core.IFundingMonitor,
	logger core.ILogger,
	cfg EngineConfig,
) engine.Engine {
	logicCfg := arbitrage.StrategyConfig{
		MinSpreadAPR:         cfg.MinSpreadAPR,
		ExitSpreadAPR:        cfg.ExitSpreadAPR,
		LiquidationThreshold: cfg.LiquidationThreshold,
		UMWarningThreshold:   cfg.UMWarningThreshold,
		UMEmergencyThreshold: cfg.UMEmergencyThreshold,
		ToxicBasisThreshold:  cfg.ToxicBasisThreshold,
	}

	return &ArbitrageEngine{
		exchanges:        exchanges,
		monitor:          monitor,
		fundingMonitor:   fundingMonitor,
		logger:           logger.WithField("component", "arbitrage_engine"),
		strategy:         arbitrage.NewStrategy(logicCfg),
		legManager:       arbitrage.NewLegManager(exchanges, logger),
		executor:         execution.NewSequenceExecutor(exchanges, logger),
		parallelExecutor: execution.NewParallelExecutor(exchanges, logger),
		symbol:           cfg.Symbol,
		spotExchange:     cfg.SpotExchange,
		perpExchange:     cfg.PerpExchange,
		orderQuantity:    cfg.OrderQuantity,
		stalenessTTL:     cfg.FundingStalenessThreshold,
	}
}

func (e *ArbitrageEngine) Start(ctx context.Context) error {
	e.logger.Info("Starting Arbitrage Engine")

	// Sync State
	if err := e.legManager.SyncState(ctx, e.spotExchange, e.symbol); err != nil {
		return err
	}
	if err := e.legManager.SyncState(ctx, e.perpExchange, e.symbol); err != nil {
		return err
	}

	return nil
}

func (e *ArbitrageEngine) Stop() error {
	return nil
}

func (e *ArbitrageEngine) OnPriceUpdate(ctx context.Context, price *pb.PriceChange) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if price.Symbol == e.symbol {
		if price.Exchange == e.spotExchange {
			e.lastSpotPrice = pbu.ToGoDecimal(price.Price)
		} else if price.Exchange == e.perpExchange {
			e.lastPerpPrice = pbu.ToGoDecimal(price.Price)
		}
	}
	return nil
}

func (e *ArbitrageEngine) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	return e.legManager.SyncState(ctx, update.Exchange, update.Symbol)
}

func (e *ArbitrageEngine) OnFundingUpdate(ctx context.Context, update *pb.FundingUpdate) error {
	// Only care about our exchanges
	if (update.Exchange != e.perpExchange && update.Exchange != e.spotExchange) || update.Symbol != e.symbol {
		return nil
	}

	e.mu.Lock()
	// Deduplicate by NextFundingTime
	if update.NextFundingTime <= e.lastNextFundingTime && update.NextFundingTime != 0 {
		e.mu.Unlock()
		return nil
	}

	// Check if already executing
	if e.isExecuting {
		e.mu.Unlock()
		return nil
	}

	// Check staleness for both legs
	if e.fundingMonitor.IsStale(e.spotExchange, e.symbol, e.stalenessTTL) {
		e.logger.Warn("Spot funding stale, skipping decision", "exchange", e.spotExchange)
		e.mu.Unlock()
		return nil
	}
	if e.fundingMonitor.IsStale(e.perpExchange, e.symbol, e.stalenessTTL) {
		e.logger.Warn("Perp funding stale, skipping decision", "exchange", e.perpExchange)
		e.mu.Unlock()
		return nil
	}

	// Get rates
	spotRate, err := e.fundingMonitor.GetRate(e.spotExchange, e.symbol)
	if err != nil {
		e.logger.Error("Failed to get spot rate", "error", err)
		e.mu.Unlock()
		return nil
	}
	perpRate, err := e.fundingMonitor.GetRate(e.perpExchange, e.symbol)
	if err != nil {
		e.logger.Error("Failed to get perp rate", "error", err)
		e.mu.Unlock()
		return nil
	}

	// Compute spread and APR
	spread := arbitrage.ComputeSpread(spotRate, perpRate)
	// For now assume 8h interval
	apr := arbitrage.AnnualizeSpread(spread, decimal.NewFromInt(8))

	isPositionOpen := e.legManager.HasOpenPosition(e.symbol)

	// Basis Stop Check
	if isPositionOpen && !e.lastSpotPrice.IsZero() && !e.lastPerpPrice.IsZero() {
		basisAction := e.strategy.EvaluateBasis(e.lastSpotPrice, e.lastPerpPrice)
		if basisAction == arbitrage.ActionToxicExit {
			e.toxicBasisCount++
			telemetry.GetGlobalMetrics().SetToxicBasisCount(e.symbol, int64(e.toxicBasisCount))
			if e.toxicBasisCount >= 3 {
				e.logger.Warn("Strategy: TOXIC BASIS EXIT signaled (Basis Stop)",
					"spot", e.lastSpotPrice.String(),
					"perp", e.lastPerpPrice.String(),
					"count", e.toxicBasisCount)

				e.isExecuting = true
				e.mu.Unlock()
				err := e.executeExit(ctx, update.NextFundingTime)
				e.mu.Lock()
				e.isExecuting = false
				if err == nil {
					e.lastNextFundingTime = update.NextFundingTime
				}
				e.mu.Unlock()
				return err
			}
		} else {
			e.toxicBasisCount = 0
			telemetry.GetGlobalMetrics().SetToxicBasisCount(e.symbol, 0)
		}
	}

	// Risk Check
	isRiskTriggered := false
	if e.monitor != nil {
		isRiskTriggered = e.monitor.IsTriggered()
	}

	if isRiskTriggered {
		e.logger.Warn("Risk triggered, skipping entry")
		e.mu.Unlock()
		return nil
	}

	action := e.strategy.CalculateAction(apr, isPositionOpen)

	// 2. Determine Target Position
	targetSize := decimal.Zero

	switch action {
	case arbitrage.ActionEntryPositive:
		// Target Long Spot / Short Perp (Positive Funding)
		targetSize = e.orderQuantity
	case arbitrage.ActionEntryNegative:
		// Target Short Spot / Long Perp (Negative Funding)
		targetSize = e.orderQuantity.Neg()
	case arbitrage.ActionExit, arbitrage.ActionToxicExit:
		targetSize = decimal.Zero
	case arbitrage.ActionReduceExposure:
		// Reduce by 50%
		spotSize := e.legManager.GetSize(e.spotExchange, e.symbol)
		if spotSize.IsZero() {
			targetSize = decimal.Zero
		} else {
			targetSize = spotSize.Mul(decimal.NewFromFloat(0.5))
		}
	case arbitrage.ActionNone:
		// Maintain current position
		currentSigned := e.legManager.GetSignedSize(e.spotExchange, e.symbol)
		targetSize = currentSigned
	}

	target := &core.TargetState{
		Positions: []core.TargetPosition{
			{Exchange: e.spotExchange, Symbol: e.symbol, Size: targetSize},
			{Exchange: e.perpExchange, Symbol: e.symbol, Size: targetSize.Neg()},
		},
	}

	// 3. Reconcile
	e.isExecuting = true
	e.mu.Unlock()
	e.reconcile(ctx, target, update.NextFundingTime)

	e.mu.Lock()
	e.isExecuting = false
	e.mu.Unlock()
	return nil
}

func (e *ArbitrageEngine) GetStatus() (*core.TargetState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Construct a snapshot of the current state
	// For now, return what we have in memory as target
	return &core.TargetState{
		Positions: []core.TargetPosition{
			{Exchange: e.spotExchange, Symbol: e.symbol, Size: e.legManager.GetSize(e.spotExchange, e.symbol)},
			{Exchange: e.perpExchange, Symbol: e.symbol, Size: e.legManager.GetSize(e.perpExchange, e.symbol)},
		},
	}, nil
}

func (e *ArbitrageEngine) reconcile(ctx context.Context, target *core.TargetState, nextFundingTime int64) {
	// Simple Delta Reconciliation
	spotTarget := target.Positions[0].Size
	spotCurrent := e.legManager.GetSignedSize(e.spotExchange, e.symbol)

	// Calculate Delta
	// Delta = Target - Current
	delta := spotTarget.Sub(spotCurrent)

	// Threshold for action (dust check)
	if delta.Abs().LessThan(decimal.NewFromFloat(0.0001)) {
		return
	}

	e.logger.Info("Reconciling", "target", spotTarget, "current", spotCurrent, "delta", delta)

	// Determine Sides
	isPositive := delta.IsPositive() // Buying spot

	// Execute Entry/Exit based on Delta
	if isPositive {
		// Buying Spot (Entry Positive or Exit Negative)
		// Check if we are increasing exposure or decreasing
		if spotCurrent.IsNegative() {
			// Closing Short Spot -> Exit Negative
			// But executeExit closes ALL. We need partial.
			// Let's use the atomic entry logic for new positions
			if spotCurrent.IsZero() {
				// New Entry
				_ = e.executeEntry(ctx, true, nextFundingTime)
			} else {
				// Partial close logic (TODO: Refactor executeExit/Entry to handle quantity)
				// For now, if we need to close, we call executeExit which closes everything
				_ = e.executeExit(ctx, nextFundingTime)
			}
		} else {
			// Increasing Long Spot -> Entry Positive
			_ = e.executeEntry(ctx, true, nextFundingTime)
		}
	} else {
		// Selling Spot (Entry Negative or Exit Positive)
		if spotCurrent.IsPositive() {
			// Closing Long Spot -> Exit Positive
			_ = e.executeExit(ctx, nextFundingTime)
		} else {
			// Increasing Short Spot -> Entry Negative
			_ = e.executeEntry(ctx, false, nextFundingTime)
		}
	}
}

func (e *ArbitrageEngine) updateDeltaNeutralityMetrics() {
	spotSize := e.legManager.GetSize(e.spotExchange, e.symbol)
	perpSize := e.legManager.GetSize(e.perpExchange, e.symbol)

	// In funding arb, spot and perp should have opposite signs.
	// Total Delta = Spot + Perp
	delta := spotSize.Add(perpSize)
	absDelta, _ := delta.Abs().Float64()

	totalSize := spotSize.Abs().Add(perpSize.Abs())
	totalSizeF, _ := totalSize.Float64()

	neutrality := 1.0
	if totalSizeF > 0 {
		neutrality = 1.0 - (absDelta / totalSizeF)
	}

	telemetry.GetGlobalMetrics().SetDeltaNeutrality(e.symbol, neutrality)
}

func (e *ArbitrageEngine) OnPositionUpdate(ctx context.Context, pos *pb.Position) error {
	if pos.Symbol != e.symbol {
		return nil
	}

	e.mu.Lock()
	e.updateDeltaNeutralityMetrics()

	if e.strategy.ShouldEmergencyExit(pos) {
		e.logger.Warn("Strategy: EMERGENCY EXIT signaled (Liquidation Guard)")
		if e.isExecuting {
			e.mu.Unlock()
			return nil
		}
		e.isExecuting = true
		e.mu.Unlock()
		err := e.executeExit(ctx, 0) // Use 0 for emergency
		e.mu.Lock()
		e.isExecuting = false
		e.mu.Unlock()
		return err
	}

	e.mu.Unlock()
	return nil
}

func (e *ArbitrageEngine) SetOrderQuantity(qty decimal.Decimal) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.orderQuantity = qty
}

func (e *ArbitrageEngine) GetOrderQuantity() decimal.Decimal {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.orderQuantity
}

func (e *ArbitrageEngine) OnAccountUpdate(ctx context.Context, account *pb.Account) error {
	e.mu.Lock()
	if e.isExecuting {
		e.mu.Unlock()
		return nil
	}

	action := e.strategy.EvaluateUMAccountHealth(account)
	switch action {
	case arbitrage.ActionExit:
		e.logger.Warn("UM Account Health Critical, initiating emergency exit")
		e.isExecuting = true
		e.mu.Unlock()
		err := e.executeExit(ctx, 0)
		e.mu.Lock()
		e.isExecuting = false
		e.mu.Unlock()
		return err

	case arbitrage.ActionReduceExposure:
		e.logger.Warn("UM Account Health Warning, reducing exposure by 50%")
		e.isExecuting = true
		e.mu.Unlock()
		err := e.executePartialExit(ctx, decimal.NewFromFloat(0.5))
		e.mu.Lock()
		e.isExecuting = false
		e.mu.Unlock()
		return err
	}

	e.mu.Unlock()
	return nil
}

func (e *ArbitrageEngine) calculateAggressivePrice(price decimal.Decimal, side pb.OrderSide) decimal.Decimal {
	if price.IsZero() {
		return decimal.Zero
	}

	// Offset by 0.5% for aggressive fill
	offset := price.Mul(decimal.NewFromFloat(0.005))
	if side == pb.OrderSide_ORDER_SIDE_BUY {
		return price.Add(offset)
	}
	return price.Sub(offset)
}

func (e *ArbitrageEngine) executePartialExit(ctx context.Context, ratio decimal.Decimal) error {
	// Re-sync to get latest sizes
	if err := e.legManager.SyncState(ctx, e.spotExchange, e.symbol); err != nil {
		return err
	}
	if err := e.legManager.SyncState(ctx, e.perpExchange, e.symbol); err != nil {
		return err
	}

	e.mu.Lock()
	spotPrice := e.calculateAggressivePrice(e.lastSpotPrice, pb.OrderSide_ORDER_SIDE_SELL)
	perpPrice := e.calculateAggressivePrice(e.lastPerpPrice, pb.OrderSide_ORDER_SIDE_BUY)

	// Recalculate sides correctly based on current positions
	spotSide := pb.OrderSide_ORDER_SIDE_SELL
	if e.legManager.GetSide(e.spotExchange, e.symbol) == pb.OrderSide_ORDER_SIDE_SELL {
		spotSide = pb.OrderSide_ORDER_SIDE_BUY
		spotPrice = e.calculateAggressivePrice(e.lastSpotPrice, spotSide)
	}
	perpSide := pb.OrderSide_ORDER_SIDE_SELL
	if e.legManager.GetSide(e.perpExchange, e.symbol) == pb.OrderSide_ORDER_SIDE_SELL {
		perpSide = pb.OrderSide_ORDER_SIDE_BUY
		perpPrice = e.calculateAggressivePrice(e.lastPerpPrice, perpSide)
	}

	// Calculate partial sizes
	spotSize := e.legManager.GetSize(e.spotExchange, e.symbol).Mul(ratio).Abs()
	perpSize := e.legManager.GetSize(e.perpExchange, e.symbol).Mul(ratio).Abs()
	e.mu.Unlock()

	if spotSize.IsZero() && perpSize.IsZero() {
		return nil
	}

	e.logger.Info("Executing Partial Exit", "ratio", ratio.String(), "spotSize", spotSize.String(), "perpSize", perpSize.String())

	// Determine order types
	spotType := pb.OrderType_ORDER_TYPE_LIMIT
	perpType := pb.OrderType_ORDER_TYPE_LIMIT
	if spotPrice.IsZero() {
		spotType = pb.OrderType_ORDER_TYPE_MARKET
	}
	if perpPrice.IsZero() {
		perpType = pb.OrderType_ORDER_TYPE_MARKET
	}

	// If same exchange, use batch
	if e.spotExchange == e.perpExchange {
		ex, ok := e.exchanges[e.spotExchange]
		if ok && ex.IsUnifiedMargin() {
			reqs := []*pb.PlaceOrderRequest{
				{
					Symbol:        e.symbol,
					Side:          perpSide,
					Type:          perpType,
					Price:         pbu.FromGoDecimal(perpPrice),
					Quantity:      pbu.FromGoDecimal(perpSize),
					ClientOrderId: e.generateClientOrderID("reduce_perp", 0),
					ReduceOnly:    true,
				},
				{
					Symbol:        e.symbol,
					Side:          spotSide,
					Type:          spotType,
					Price:         pbu.FromGoDecimal(spotPrice),
					Quantity:      pbu.FromGoDecimal(spotSize),
					ClientOrderId: e.generateClientOrderID("reduce_spot", 0),
					UseMargin:     true, // In UM we use margin for everything usually
				},
			}
			_, allSuccess := ex.BatchPlaceOrders(ctx, reqs)
			if !allSuccess {
				return fmt.Errorf("same-exchange batch reduction failed")
			}
			return nil
		}
	}

	// Cross-exchange or non-UM
	steps := []execution.Step{
		{
			Exchange: e.perpExchange,
			Request: &pb.PlaceOrderRequest{
				Symbol:        e.symbol,
				Side:          perpSide,
				Type:          perpType,
				Price:         pbu.FromGoDecimal(perpPrice),
				Quantity:      pbu.FromGoDecimal(perpSize),
				ClientOrderId: e.generateClientOrderID("reduce_perp", 0),
				ReduceOnly:    true,
			},
		},
		{
			Exchange: e.spotExchange,
			Request: &pb.PlaceOrderRequest{
				Symbol:        e.symbol,
				Side:          spotSide,
				Type:          spotType,
				Price:         pbu.FromGoDecimal(spotPrice),
				Quantity:      pbu.FromGoDecimal(spotSize),
				ClientOrderId: e.generateClientOrderID("reduce_spot", 0),
				UseMargin:     true,
			},
		},
	}

	return e.executor.Execute(ctx, steps)
}

func (e *ArbitrageEngine) generateClientOrderID(prefix string, nextFundingTime int64) string {
	return fmt.Sprintf("arb_%s_%s_%d", prefix, e.symbol, nextFundingTime)
}

func (e *ArbitrageEngine) executeEntry(ctx context.Context, isPositive bool, nextFundingTime int64) error {
	spotSide := pb.OrderSide_ORDER_SIDE_BUY
	perpSide := pb.OrderSide_ORDER_SIDE_SELL
	compensateSide := pb.OrderSide_ORDER_SIDE_SELL

	if !isPositive {
		spotSide = pb.OrderSide_ORDER_SIDE_SELL
		perpSide = pb.OrderSide_ORDER_SIDE_BUY
		compensateSide = pb.OrderSide_ORDER_SIDE_BUY
	}

	// Case 1: Same exchange UM Arb
	if e.spotExchange == e.perpExchange {
		ex, ok := e.exchanges[e.spotExchange]
		if ok && ex.IsUnifiedMargin() {
			e.logger.Info("Executing Same-Exchange Unified Entry", "exchange", e.spotExchange)
			reqs := []*pb.PlaceOrderRequest{
				{
					Symbol:        e.symbol,
					Side:          spotSide,
					Type:          pb.OrderType_ORDER_TYPE_MARKET,
					Quantity:      pbu.FromGoDecimal(e.orderQuantity),
					ClientOrderId: e.generateClientOrderID("spot", nextFundingTime),
					UseMargin:     !isPositive,
				},
				{
					Symbol:        e.symbol,
					Side:          perpSide,
					Type:          pb.OrderType_ORDER_TYPE_MARKET,
					Quantity:      pbu.FromGoDecimal(e.orderQuantity),
					ClientOrderId: e.generateClientOrderID("perp", nextFundingTime),
				},
			}
			_, allSuccess := ex.BatchPlaceOrders(ctx, reqs)
			if !allSuccess {
				return fmt.Errorf("same-exchange batch entry failed")
			}
			return nil
		}
	}

	// Case 2: Cross-exchange or non-UM
	// Atomic Neutrality: Place Spot first, then scale Perp to actual fill
	e.logger.Info("Executing Cross-Exchange Atomic Entry", "spot", e.spotExchange, "perp", e.perpExchange)

	spotEx := e.exchanges[e.spotExchange]
	spotOrder, err := spotEx.PlaceOrder(ctx, &pb.PlaceOrderRequest{
		Symbol:        e.symbol,
		Side:          spotSide,
		Type:          pb.OrderType_ORDER_TYPE_MARKET,
		Quantity:      pbu.FromGoDecimal(e.orderQuantity),
		ClientOrderId: e.generateClientOrderID("spot", nextFundingTime),
		UseMargin:     !isPositive,
	})
	if err != nil {
		return fmt.Errorf("spot entry leg failed: %w", err)
	}

	execQty := pbu.ToGoDecimal(spotOrder.ExecutedQty)
	if execQty.IsZero() {
		return fmt.Errorf("spot entry leg failed: zero fill")
	}

	e.logger.Info("Spot leg filled, placing matching perp leg", "qty", execQty.String())

	perpEx := e.exchanges[e.perpExchange]
	_, err = perpEx.PlaceOrder(ctx, &pb.PlaceOrderRequest{
		Symbol:        e.symbol,
		Side:          perpSide,
		Type:          pb.OrderType_ORDER_TYPE_MARKET,
		Quantity:      pbu.FromGoDecimal(execQty),
		ClientOrderId: e.generateClientOrderID("perp", nextFundingTime),
	})
	if err != nil {
		e.logger.Error("Perp leg failed after spot fill, initiating compensation", "error", err)
		// Compensate spot
		_, compErr := spotEx.PlaceOrder(ctx, &pb.PlaceOrderRequest{
			Symbol:    e.symbol,
			Side:      compensateSide,
			Type:      pb.OrderType_ORDER_TYPE_MARKET,
			Quantity:  pbu.FromGoDecimal(execQty),
			UseMargin: !isPositive,
		})
		if compErr != nil {
			e.logger.Error("CRITICAL: Spot compensation failed", "error", compErr)
		}
		return err
	}

	return nil
}

func (e *ArbitrageEngine) executeExit(ctx context.Context, nextFundingTime int64) error {
	// Re-sync to get latest sizes
	if err := e.legManager.SyncState(ctx, e.spotExchange, e.symbol); err != nil {
		return err
	}
	if err := e.legManager.SyncState(ctx, e.perpExchange, e.symbol); err != nil {
		return err
	}

	// Determine sides from LegManager
	spotSide := pb.OrderSide_ORDER_SIDE_SELL // Default close for Long
	perpSide := pb.OrderSide_ORDER_SIDE_BUY  // Default close for Short

	currentSpotSide := e.legManager.GetSide(e.spotExchange, e.symbol)
	if currentSpotSide == pb.OrderSide_ORDER_SIDE_SELL {
		spotSide = pb.OrderSide_ORDER_SIDE_BUY
	}

	currentPerpSide := e.legManager.GetSide(e.perpExchange, e.symbol)
	if currentPerpSide == pb.OrderSide_ORDER_SIDE_BUY {
		perpSide = pb.OrderSide_ORDER_SIDE_SELL
	}

	e.mu.Lock()
	// Get aggressive limit prices
	spotPrice := e.calculateAggressivePrice(e.lastSpotPrice, spotSide)
	perpPrice := e.calculateAggressivePrice(e.lastPerpPrice, perpSide)

	qty := e.orderQuantity
	e.mu.Unlock()

	// Determine order types
	spotType := pb.OrderType_ORDER_TYPE_LIMIT
	perpType := pb.OrderType_ORDER_TYPE_LIMIT
	if spotPrice.IsZero() {
		spotType = pb.OrderType_ORDER_TYPE_MARKET
	}
	if perpPrice.IsZero() {
		perpType = pb.OrderType_ORDER_TYPE_MARKET
	}

	// If same exchange, use batch
	if e.spotExchange == e.perpExchange {
		ex, ok := e.exchanges[e.spotExchange]
		if ok && ex.IsUnifiedMargin() {
			e.logger.Info("Executing Same-Exchange Unified Exit", "exchange", e.spotExchange)

			reqs := []*pb.PlaceOrderRequest{
				{
					Symbol:        e.symbol,
					Side:          perpSide,
					Type:          perpType,
					Price:         pbu.FromGoDecimal(perpPrice),
					Quantity:      pbu.FromGoDecimal(qty),
					ClientOrderId: e.generateClientOrderID("exit_perp", nextFundingTime),
				},
				{
					Symbol:        e.symbol,
					Side:          spotSide,
					Type:          spotType,
					Price:         pbu.FromGoDecimal(spotPrice),
					Quantity:      pbu.FromGoDecimal(qty),
					ClientOrderId: e.generateClientOrderID("exit_spot", nextFundingTime),
				},
			}
			_, allSuccess := ex.BatchPlaceOrders(ctx, reqs)
			if !allSuccess {
				return fmt.Errorf("same-exchange batch exit failed")
			}
			return nil
		}
	}

	// Cross-exchange or non-UM
	steps := []execution.Step{
		{
			Exchange: e.perpExchange,
			Request: &pb.PlaceOrderRequest{
				Symbol:        e.symbol,
				Side:          perpSide,
				Type:          perpType,
				Price:         pbu.FromGoDecimal(perpPrice),
				Quantity:      pbu.FromGoDecimal(qty),
				ClientOrderId: e.generateClientOrderID("exit_perp", nextFundingTime),
			},
		},
		{
			Exchange: e.spotExchange,
			Request: &pb.PlaceOrderRequest{
				Symbol:        e.symbol,
				Side:          spotSide,
				Type:          spotType,
				Price:         pbu.FromGoDecimal(spotPrice),
				Quantity:      pbu.FromGoDecimal(qty),
				ClientOrderId: e.generateClientOrderID("exit_spot", nextFundingTime),
				UseMargin:     true,
			},
		},
	}

	return e.executor.Execute(ctx, steps)
}
