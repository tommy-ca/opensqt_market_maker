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

	action := e.strategy.CalculateAction(apr, isPositionOpen)

	switch action {
	case arbitrage.ActionEntryPositive:
		if isRiskTriggered {
			e.logger.Warn("Risk triggered, skipping entry")
			e.mu.Unlock()
			return nil
		}
		e.logger.Info("Strategy: ENTRY (Positive) signaled", "apr", apr.String())
		e.isExecuting = true
		e.mu.Unlock()
		err := e.executeEntry(ctx, true, update.NextFundingTime)
		e.mu.Lock()
		e.isExecuting = false
		if err == nil {
			e.lastNextFundingTime = update.NextFundingTime
		}
		e.mu.Unlock()
		return err

	case arbitrage.ActionEntryNegative:
		if isRiskTriggered {
			e.logger.Warn("Risk triggered, skipping entry")
			e.mu.Unlock()
			return nil
		}
		e.logger.Info("Strategy: ENTRY (Negative) signaled", "apr", apr.String())
		e.isExecuting = true
		e.mu.Unlock()
		err := e.executeEntry(ctx, false, update.NextFundingTime)
		e.mu.Lock()
		e.isExecuting = false
		if err == nil {
			e.lastNextFundingTime = update.NextFundingTime
		}
		e.mu.Unlock()
		return err

	case arbitrage.ActionExit:
		e.logger.Info("Strategy: EXIT signaled", "apr", apr.String())
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

	e.mu.Unlock()
	return nil
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

func (e *ArbitrageEngine) getAggressiveLimitPrice(exchange string, side pb.OrderSide) decimal.Decimal {
	e.mu.Lock()
	defer e.mu.Unlock()

	var price decimal.Decimal
	if exchange == e.spotExchange {
		price = e.lastSpotPrice
	} else if exchange == e.perpExchange {
		price = e.lastPerpPrice
	}

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

	// Calculate partial sizes
	spotSize := e.legManager.GetSize(e.spotExchange, e.symbol).Mul(ratio).Abs()
	perpSize := e.legManager.GetSize(e.perpExchange, e.symbol).Mul(ratio).Abs()

	if spotSize.IsZero() && perpSize.IsZero() {
		return nil
	}

	e.logger.Info("Executing Partial Exit", "ratio", ratio.String(), "spotSize", spotSize.String(), "perpSize", perpSize.String())

	// Determine sides
	spotSide := pb.OrderSide_ORDER_SIDE_SELL
	if e.legManager.GetSide(e.spotExchange, e.symbol) == pb.OrderSide_ORDER_SIDE_SELL {
		spotSide = pb.OrderSide_ORDER_SIDE_BUY
	}
	perpSide := pb.OrderSide_ORDER_SIDE_SELL
	if e.legManager.GetSide(e.perpExchange, e.symbol) == pb.OrderSide_ORDER_SIDE_SELL {
		perpSide = pb.OrderSide_ORDER_SIDE_BUY
	}

	// Get aggressive limit prices
	spotPrice := e.getAggressiveLimitPrice(e.spotExchange, spotSide)
	perpPrice := e.getAggressiveLimitPrice(e.perpExchange, perpSide)

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

	// Get aggressive limit prices
	spotPrice := e.getAggressiveLimitPrice(e.spotExchange, spotSide)
	perpPrice := e.getAggressiveLimitPrice(e.perpExchange, perpSide)

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
			e.mu.Lock()
			qty := e.orderQuantity
			e.mu.Unlock()

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

	// Case 2: Cross-exchange or non-UM
	// Parallel Execution: Close both legs concurrently to minimize slippage
	e.logger.Info("Executing Cross-Exchange Parallel Exit", "spot", e.spotExchange, "perp", e.perpExchange)

	// Get current sizes
	spotQty := e.legManager.GetSize(e.spotExchange, e.symbol).Abs()
	perpQty := e.legManager.GetSize(e.perpExchange, e.symbol).Abs()

	steps := []execution.Step{
		{
			Exchange: e.perpExchange,
			Request: &pb.PlaceOrderRequest{
				Symbol:        e.symbol,
				Side:          perpSide,
				Type:          perpType,
				Price:         pbu.FromGoDecimal(perpPrice),
				Quantity:      pbu.FromGoDecimal(perpQty),
				ClientOrderId: e.generateClientOrderID("exit_perp", nextFundingTime),
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
				Quantity:      pbu.FromGoDecimal(spotQty),
				ClientOrderId: e.generateClientOrderID("exit_spot", nextFundingTime),
				UseMargin:     true, // Closing a margin position
			},
		},
	}
	return e.parallelExecutor.Execute(ctx, steps)
}
