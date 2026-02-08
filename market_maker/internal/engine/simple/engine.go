package simple

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/engine"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"market_maker/pkg/telemetry"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"
)

// SimpleEngine implements the engine.Engine interface
type SimpleEngine struct {
	store           core.IStateStore
	positionManager core.IPositionManager
	orderExecutor   core.IOrderExecutor
	riskMonitor     core.IRiskMonitor
	logger          core.ILogger

	mu sync.Mutex

	// OTel
	tracer       trace.Tracer
	priceCounter metric.Int64Counter
	orderCounter metric.Int64Counter
	latencyHist  metric.Float64Histogram

	// State tracking
	lastTriggered  bool
	currentPrice   *pb.PriceChange
	currentVersion int64
}

// NewSimpleEngine creates a new workflow engine
func NewSimpleEngine(
	store core.IStateStore,
	pm core.IPositionManager,
	oe core.IOrderExecutor,
	rm core.IRiskMonitor,
	logger core.ILogger,
) engine.Engine {
	tracer := telemetry.GetTracer("workflow-engine")
	meter := telemetry.GetMeter("workflow-engine")

	priceCounter, _ := meter.Int64Counter("workflow_price_updates_total",
		metric.WithDescription("Total number of price updates processed"))
	orderCounter, _ := meter.Int64Counter("workflow_order_updates_total",
		metric.WithDescription("Total number of order updates processed"))
	latencyHist, _ := meter.Float64Histogram("workflow_processing_latency_seconds",
		metric.WithDescription("Latency of processing updates in seconds"))

	return &SimpleEngine{
		store:           store,
		positionManager: pm,
		orderExecutor:   oe,
		riskMonitor:     rm,
		logger:          logger,
		tracer:          tracer,
		priceCounter:    priceCounter,
		orderCounter:    orderCounter,
		latencyHist:     latencyHist,
	}
}

// Start starts the engine and attempts to restore state
func (e *SimpleEngine) Start(ctx context.Context) error {
	e.logger.Info("Starting workflow engine")

	// Attempt to load state
	state, err := e.store.LoadState(ctx)
	if err == nil && state != nil {
		e.logger.Info("Found persisted state, restoring...")
		if err := e.positionManager.RestoreState(state.Slots); err != nil {
			e.logger.Error("Failed to restore state", "error", err)
			return fmt.Errorf("failed to restore state: %w", err)
		}
		e.currentVersion = state.Version
		if state.LastPrice != nil {
			e.currentPrice = &pb.PriceChange{
				Price:  state.LastPrice,
				Symbol: state.Symbol,
			}
		}
		e.logger.Info("State restored successfully", "version", e.currentVersion)
	} else {
		e.logger.Info("No persisted state found, starting fresh")
	}

	// Exchange Reconciliation (The Reality Check)
	// SimpleEngine needs access to an exchange to reconcile
	// Currently it doesn't store one. I'll add it if needed or just skip here
	// for generic workflow engine.

	return nil
}

// Stop stops the engine
func (e *SimpleEngine) Stop() error {
	e.logger.Info("Stopping workflow engine")
	return nil
}

func (e *SimpleEngine) GetPositionManager() core.IPositionManager {
	return e.positionManager
}

// OnPriceUpdate processes a price update event
func (e *SimpleEngine) OnPriceUpdate(ctx context.Context, price *pb.PriceChange) error {
	start := time.Now()
	pVal := pbu.ToGoDecimal(price.Price)
	pFloat, _ := pVal.Float64()
	ctx, span := e.tracer.Start(ctx, "OnPriceUpdate",
		trace.WithAttributes(
			attribute.String("symbol", price.Symbol),
			attribute.Float64("price", pFloat),
		),
	)
	defer span.End()

	e.mu.Lock()
	defer e.mu.Unlock()

	var allResults []core.OrderActionResult

	// 1. Risk Check & Handling
	if e.riskMonitor != nil {
		isTriggered := e.riskMonitor.IsTriggered()

		// Transition: Normal -> Triggered
		if isTriggered && !e.lastTriggered {
			e.logger.Warn("🚨 Risk monitor triggered! Cancelling all BUY orders...")
			span.AddEvent("risk_triggered_start")

			// Cancel all buy orders
			actions, err := e.positionManager.CancelAllBuyOrders(ctx)
			if err != nil {
				e.logger.Error("Failed to generate cancel actions", "error", err)
			} else if len(actions) > 0 {
				allResults = append(allResults, e.executeActions(ctx, actions)...)
			}
		}

		// Transition: Triggered -> Normal
		if !isTriggered && e.lastTriggered {
			e.logger.Info("✅ Risk monitor cleared. Resuming normal trading.")
			span.AddEvent("risk_triggered_end")
		}

		e.lastTriggered = isTriggered

		if isTriggered {
			// Skip normal adjustments while triggered
			goto SaveState
		}
	}

	// 2. Calculate Adjustments (Deterministic Decision)
	{
		actions, err := e.positionManager.CalculateAdjustments(ctx, pVal)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to calculate adjustments: %w", err)
		}

		// 3. Execute Actions (Side Effects)
		if len(actions) > 0 {
			allResults = append(allResults, e.executeActions(ctx, actions)...)
		}
	}

SaveState:
	// 4. Build New State Snapshot (Persist First)
	newState := e.buildStateSnapshot(price)
	if len(allResults) > 0 {
		e.applyResultsToState(newState, allResults)
	}

	// 5. Save State
	if err := e.store.SaveState(ctx, newState); err != nil {
		e.logger.Error("Failed to save state", "error", err)
		span.RecordError(err)
		return err // In-memory state NOT changed yet - safe to return
	}

	// 6. ONLY AFTER successful persistence, update in-memory state
	if len(allResults) > 0 {
		if err := e.positionManager.ApplyActionResults(allResults); err != nil {
			e.logger.Error("CRITICAL: In-memory update failed after persistence", "error", err)
			span.RecordError(err)
			return fmt.Errorf("critical state desync: %w", err)
		}
	}

	e.currentPrice = price

	// Metrics
	duration := time.Since(start).Seconds()
	e.priceCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("symbol", price.Symbol)))
	e.latencyHist.Record(ctx, duration, metric.WithAttributes(attribute.String("type", "price_update")))

	return nil
}

// OnOrderUpdate processes an order update event
func (e *SimpleEngine) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	start := time.Now()
	ctx, span := e.tracer.Start(ctx, "OnOrderUpdate",
		trace.WithAttributes(
			attribute.Int64("order_id", update.OrderId),
			attribute.String("symbol", update.Symbol),
			attribute.String("status", string(update.Status)),
		),
	)
	defer span.End()

	e.mu.Lock()
	defer e.mu.Unlock()

	// 1. Build New State Snapshot (Persist First)
	newState := e.buildStateSnapshot(nil)
	if err := e.applyOrderUpdateToState(newState, update); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to preview order update: %w", err)
	}

	// 2. Save State
	if err := e.store.SaveState(ctx, newState); err != nil {
		e.logger.Error("Failed to save state before order update", "error", err)
		span.RecordError(err)
		return err // In-memory state NOT changed yet - safe to return
	}

	// 3. Update Live State
	if err := e.positionManager.OnOrderUpdate(ctx, update); err != nil {
		// This should theoretically not fail if preview succeeded
		e.logger.Error("CRITICAL: In-memory update failed after persistence", "error", err)
		span.RecordError(err)
		return fmt.Errorf("critical state desync: %w", err)
	}

	// Metrics
	duration := time.Since(start).Seconds()
	e.orderCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("symbol", update.Symbol),
		attribute.String("status", string(update.Status)),
	))
	e.latencyHist.Record(ctx, duration, metric.WithAttributes(attribute.String("type", "order_update")))

	return nil
}

// OnFundingUpdate processes a funding rate update event
func (e *SimpleEngine) OnFundingUpdate(ctx context.Context, update *pb.FundingUpdate) error {
	// SimpleEngine (Grid) currently ignores funding updates
	return nil
}

// OnPositionUpdate processes a position update event
func (e *SimpleEngine) OnPositionUpdate(ctx context.Context, position *pb.Position) error {
	// SimpleEngine tracks position via Order updates and PositionManager state,
	// but ignoring direct position stream for now to avoid double counting or conflict.
	return nil
}

// OnAccountUpdate processes an account update event
func (e *SimpleEngine) OnAccountUpdate(ctx context.Context, account *pb.Account) error {
	return nil
}

func (e *SimpleEngine) executeActions(ctx context.Context, actions []*pb.OrderAction) []core.OrderActionResult {
	results := make([]core.OrderActionResult, len(actions))
	for i, action := range actions {
		res := core.OrderActionResult{Action: action}
		switch action.Type {
		case pb.OrderActionType_ORDER_ACTION_TYPE_PLACE:
			order, err := e.orderExecutor.PlaceOrder(ctx, action.Request)
			res.Order = order
			res.Error = err
		case pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL:
			err := e.orderExecutor.BatchCancelOrders(ctx, action.Symbol, []int64{action.OrderId}, false)
			res.Error = err
		}
		results[i] = res
	}
	return results
}

func (e *SimpleEngine) buildStateSnapshot(price *pb.PriceChange) *pb.State {
	slots := e.positionManager.GetSlots()
	pbSlots := make(map[string]*pb.InventorySlot)
	var symbol string
	for k, v := range slots {
		pbSlots[k] = proto.Clone(v.InventorySlot).(*pb.InventorySlot)
	}

	if price != nil {
		symbol = price.Symbol
	} else if e.currentPrice != nil {
		symbol = e.currentPrice.Symbol
	}

	e.currentVersion++
	state := &pb.State{
		Slots:          pbSlots,
		LastUpdateTime: time.Now().UnixNano(),
		Symbol:         symbol,
		Version:        e.currentVersion,
	}

	if price != nil {
		state.LastPrice = price.Price
	} else if e.currentPrice != nil {
		state.LastPrice = e.currentPrice.Price
	}

	return state
}

func (e *SimpleEngine) applyResultsToState(state *pb.State, results []core.OrderActionResult) {
	for _, res := range results {
		priceVal := pbu.ToGoDecimal(res.Action.Price)
		key := priceVal.String()
		slot, ok := state.Slots[key]
		if !ok {
			// If it doesn't exist, we skip for now as buildStateSnapshot already captures current slots
			continue
		}

		if res.Error != nil {
			slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
		} else if res.Action.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE && res.Order != nil {
			slot.OrderId = res.Order.OrderId
			slot.ClientOid = res.Order.ClientOrderId
			slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED
		}
	}
}

func (e *SimpleEngine) applyOrderUpdateToState(state *pb.State, update *pb.OrderUpdate) error {
	var targetSlot *pb.InventorySlot
	for _, s := range state.Slots {
		if s.OrderId == update.OrderId || (update.ClientOrderId != "" && s.ClientOid == update.ClientOrderId) {
			targetSlot = s
			break
		}
	}

	if targetSlot == nil {
		return fmt.Errorf("slot not found for order %d", update.OrderId)
	}

	switch update.Status {
	case pb.OrderStatus_ORDER_STATUS_FILLED:
		if targetSlot.OrderSide == pb.OrderSide_ORDER_SIDE_BUY {
			targetSlot.PositionStatus = pb.PositionStatus_POSITION_STATUS_FILLED
			targetSlot.PositionQty = update.ExecutedQty
		} else {
			targetSlot.PositionStatus = pb.PositionStatus_POSITION_STATUS_EMPTY
			targetSlot.PositionQty = pbu.FromGoDecimal(decimal.Zero)
		}
		targetSlot.OrderId = 0
		targetSlot.ClientOid = ""
		targetSlot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
	case pb.OrderStatus_ORDER_STATUS_CANCELED:
		targetSlot.OrderId = 0
		targetSlot.ClientOid = ""
		targetSlot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
	}

	return nil
}
