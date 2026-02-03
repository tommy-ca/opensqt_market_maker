// Package position provides position management functionality with slot-based trading logic
//
// LOCK ORDERING HIERARCHY:
// To prevent deadlocks, locks MUST be acquired in this order:
// 1. spm.mu (SuperPositionManager global lock)
// 2. slot.Mu (individual InventorySlot locks)
//
// RULES:
//   - NEVER acquire spm.mu while holding any slot.Mu
//   - When updating both global state (maps) and slot state:
//     a) Acquire spm.mu, read what you need, release spm.mu
//     b) Acquire slot.Mu, modify slot, release slot.Mu
//     c) Re-acquire spm.mu to update maps, release spm.mu
//   - Use RLock when only reading to allow concurrent access
//   - Keep critical sections as short as possible
package position

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/internal/trading/grid"
	"market_maker/pkg/concurrency"
	"market_maker/pkg/pbu"
	"market_maker/pkg/telemetry"
	"market_maker/pkg/tradingutils"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/protobuf/proto"
)

// SuperPositionManager implements the IPositionManager interface
type SuperPositionManager struct {
	// Configuration
	symbol         string
	exchangeName   string
	priceInterval  decimal.Decimal
	orderQuantity  decimal.Decimal
	minOrderValue  decimal.Decimal
	buyWindowSize  int
	sellWindowSize int
	priceDecimals  int
	qtyDecimals    int

	// Dependencies
	strategy       core.IStrategy
	riskMonitor    core.IRiskMonitor
	circuitBreaker core.ICircuitBreaker
	logger         core.ILogger

	// Slot management
	slots       map[string]*core.InventorySlot
	orderMap    map[int64]*core.InventorySlot  // O(1) lookup for order updates
	clientOMap  map[string]*core.InventorySlot // O(1) lookup for order updates
	anchorPrice decimal.Decimal

	// State tracking
	marginLockUntil    int64 // atomic timestamp
	marginLockDuration time.Duration

	// Idempotency tracking
	processedUpdates map[string]time.Time // updateKey → timestamp for duplicate detection
	updateMu         sync.RWMutex

	// Concurrency control
	mu sync.RWMutex

	// Statistics
	totalSlots  int64 // atomic
	activeSlots int64 // atomic

	// OTel
	meter metric.Meter

	// Update Listeners
	updateCallbacks []func(*pb.PositionUpdate)
	callbackMu      sync.RWMutex
	broadcastPool   *concurrency.WorkerPool

	// Introspection data
	fills           []*pb.Fill
	orderHistory    []*pb.Order
	positionHistory []*pb.PositionSnapshotData
	realizedPnL     decimal.Decimal
	historyMu       sync.RWMutex
}

// NewSuperPositionManager creates a new position manager instance
func NewSuperPositionManager(
	symbol string,
	exchangeName string,
	priceInterval float64,
	orderQuantity float64,
	minOrderValue float64,
	buyWindowSize int,
	sellWindowSize int,
	priceDecimals int,
	qtyDecimals int,
	strategy core.IStrategy,
	riskMonitor core.IRiskMonitor,
	circuitBreaker core.ICircuitBreaker,
	logger core.ILogger,
	broadcastPool *concurrency.WorkerPool, // Optional pool for notifications
) *SuperPositionManager {

	meter := telemetry.GetMeter("position-manager")
	spm := &SuperPositionManager{
		symbol:             symbol,
		exchangeName:       exchangeName,
		priceInterval:      decimal.NewFromFloat(priceInterval),
		orderQuantity:      decimal.NewFromFloat(orderQuantity),
		minOrderValue:      decimal.NewFromFloat(minOrderValue),
		buyWindowSize:      buyWindowSize,
		sellWindowSize:     sellWindowSize,
		priceDecimals:      priceDecimals,
		qtyDecimals:        qtyDecimals,
		strategy:           strategy,
		riskMonitor:        riskMonitor,
		circuitBreaker:     circuitBreaker,
		logger:             logger.WithField("component", "position_manager").WithField("symbol", symbol),
		slots:              make(map[string]*core.InventorySlot),
		orderMap:           make(map[int64]*core.InventorySlot),
		clientOMap:         make(map[string]*core.InventorySlot),
		marginLockDuration: 10 * time.Second, // Default 10 seconds
		processedUpdates:   make(map[string]time.Time),
		meter:              meter,
		broadcastPool:      broadcastPool,
	}

	// Register observable gauges
	commonAttrs := metric.WithAttributes(attribute.String("symbol", symbol))

	meter.Int64ObservableGauge("position_total_slots",
		metric.WithDescription("Total number of grid slots"),
		metric.WithInt64Callback(func(_ context.Context, obs metric.Int64Observer) error {
			obs.Observe(atomic.LoadInt64(&spm.totalSlots), commonAttrs)
			return nil
		}))

	meter.Int64ObservableGauge("position_active_slots",
		metric.WithDescription("Number of active grid slots (locked)"),
		metric.WithInt64Callback(func(_ context.Context, obs metric.Int64Observer) error {
			obs.Observe(atomic.LoadInt64(&spm.activeSlots), commonAttrs)
			return nil
		}))

	meter.Int64ObservableGauge("position_margin_locked",
		metric.WithDescription("Whether margin is currently locked (1 for true, 0 for false)"),
		metric.WithInt64Callback(func(_ context.Context, obs metric.Int64Observer) error {
			val := int64(0)
			if spm.isMarginLocked() {
				val = 1
			}
			obs.Observe(val, commonAttrs)
			return nil
		}))

	// Start background cleanup of old processed updates
	go spm.cleanupProcessedUpdates()

	return spm
}

// Initialize sets up the initial grid around the anchor price
func (spm *SuperPositionManager) Initialize(anchorPrice decimal.Decimal) error {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	spm.logger.Info("Initializing position manager",
		"anchor_price", anchorPrice,
		"price_interval", spm.priceInterval,
		"buy_window", spm.buyWindowSize,
		"sell_window", spm.sellWindowSize)

	spm.anchorPrice = anchorPrice
	spm.slots = make(map[string]*core.InventorySlot)
	spm.orderMap = make(map[int64]*core.InventorySlot)
	spm.clientOMap = make(map[string]*core.InventorySlot)

	buyPrices := tradingutils.CalculatePriceLevels(anchorPrice, spm.priceInterval.Neg(), spm.buyWindowSize)
	sellPrices := tradingutils.CalculatePriceLevels(anchorPrice, spm.priceInterval, spm.sellWindowSize)

	for _, price := range buyPrices {
		slot := &core.InventorySlot{
			InventorySlot: &pb.InventorySlot{
				Price:          pbu.FromGoDecimal(tradingutils.RoundPrice(price, spm.priceDecimals)),
				PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY,
				PositionQty:    pbu.FromGoDecimal(decimal.Zero),
				OrderId:        0, ClientOid: "", OrderSide: pb.OrderSide_ORDER_SIDE_BUY,
				OrderStatus: pb.OrderStatus_ORDER_STATUS_NEW, OrderPrice: pbu.FromGoDecimal(decimal.Zero),
				OrderFilledQty: pbu.FromGoDecimal(decimal.Zero), SlotStatus: pb.SlotStatus_SLOT_STATUS_FREE,
			},
		}
		spm.slots[pbu.ToGoDecimal(slot.Price).String()] = slot
		atomic.AddInt64(&spm.totalSlots, 1)
	}

	for _, price := range sellPrices {
		slot := &core.InventorySlot{
			InventorySlot: &pb.InventorySlot{
				Price:          pbu.FromGoDecimal(tradingutils.RoundPrice(price, spm.priceDecimals)),
				PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY,
				PositionQty:    pbu.FromGoDecimal(decimal.Zero),
				OrderId:        0, ClientOid: "", OrderSide: pb.OrderSide_ORDER_SIDE_SELL,
				OrderStatus: pb.OrderStatus_ORDER_STATUS_NEW, OrderPrice: pbu.FromGoDecimal(decimal.Zero),
				OrderFilledQty: pbu.FromGoDecimal(decimal.Zero), SlotStatus: pb.SlotStatus_SLOT_STATUS_FREE,
			},
		}
		spm.slots[pbu.ToGoDecimal(slot.Price).String()] = slot
		atomic.AddInt64(&spm.totalSlots, 1)
	}

	return nil
}

// RestoreState restores the position manager state from a snapshot
func (spm *SuperPositionManager) RestoreState(slots map[string]*pb.InventorySlot) error {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	if len(slots) == 0 {
		return fmt.Errorf("cannot restore empty state")
	}

	spm.slots = make(map[string]*core.InventorySlot)
	spm.orderMap = make(map[int64]*core.InventorySlot)
	spm.clientOMap = make(map[string]*core.InventorySlot)

	totalSlots := int64(0)
	for priceKey, slot := range slots {
		newSlot := &core.InventorySlot{
			InventorySlot: &pb.InventorySlot{
				Price: slot.Price, PositionStatus: slot.PositionStatus,
				PositionQty: slot.PositionQty, OrderId: slot.OrderId,
				ClientOid: slot.ClientOid, OrderSide: slot.OrderSide,
				OrderStatus: slot.OrderStatus, OrderPrice: slot.OrderPrice,
				OrderFilledQty: slot.OrderFilledQty, SlotStatus: slot.SlotStatus,
				PostOnlyFailCount: slot.PostOnlyFailCount,
			},
		}
		spm.slots[priceKey] = newSlot
		if newSlot.OrderId != 0 {
			spm.orderMap[newSlot.OrderId] = newSlot
		}
		if newSlot.ClientOid != "" {
			spm.clientOMap[newSlot.ClientOid] = newSlot
		}
		totalSlots++
	}
	atomic.StoreInt64(&spm.totalSlots, totalSlots)
	return nil
}

// RestoreFromExchangePosition distributes existing position across sell slots
func (spm *SuperPositionManager) RestoreFromExchangePosition(totalPosition decimal.Decimal) {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	defer spm.updateTelemetry() // Update telemetry after restore

	if totalPosition.LessThanOrEqual(decimal.Zero) {
		return
	}

	// 1. Calculate theoretical quantity per slot
	theoryQtyPerSlot := tradingutils.RoundQuantity(spm.orderQuantity.Div(spm.anchorPrice), spm.qtyDecimals)

	if theoryQtyPerSlot.IsZero() {
		spm.logger.Warn("Theoretical quantity is zero, cannot restore position")
		return
	}

	// 2. Calculate slots needed
	totalSlotsNeededDecimal := totalPosition.Div(theoryQtyPerSlot).Ceil()
	totalSlotsNeeded := int(totalSlotsNeededDecimal.IntPart())

	spm.logger.Info("Restoring position",
		"total", totalPosition,
		"per_slot", theoryQtyPerSlot,
		"slots_needed", totalSlotsNeeded)

	// 3. Determine Sell Window
	sellPrices := tradingutils.CalculatePriceLevels(spm.anchorPrice, spm.priceInterval, totalSlotsNeeded)

	// 4. Distribute
	var allocatedQty decimal.Decimal
	var totalTheoryQty decimal.Decimal

	theoryQtys := make([]decimal.Decimal, len(sellPrices))
	for i, price := range sellPrices {
		tQty := tradingutils.RoundQuantity(spm.orderQuantity.Div(price), spm.qtyDecimals)
		theoryQtys[i] = tQty
		totalTheoryQty = totalTheoryQty.Add(tQty)
	}

	for i, price := range sellPrices {
		var slotQty decimal.Decimal
		if i == len(sellPrices)-1 {
			slotQty = totalPosition.Sub(allocatedQty)
		} else {
			ratio := totalPosition.Div(totalTheoryQty)
			slotQty = tradingutils.RoundQuantity(theoryQtys[i].Mul(ratio), spm.qtyDecimals)

			remaining := totalPosition.Sub(allocatedQty)
			if slotQty.GreaterThan(remaining) {
				slotQty = remaining
			}
		}

		if slotQty.LessThanOrEqual(decimal.Zero) {
			continue
		}

		slot := spm.getOrCreateSlotLocked(price)

		slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_FILLED
		slot.PositionQty = pbu.FromGoDecimal(slotQty)

		slot.OrderId = 0
		slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
		slot.OrderSide = pb.OrderSide_ORDER_SIDE_SELL
		slot.ClientOid = ""
		slot.OrderFilledQty = pbu.FromGoDecimal(decimal.Zero)
		slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE

		allocatedQty = allocatedQty.Add(slotQty)
	}
}

// CalculateAdjustments calculates required order adjustments by delegating to the strategy
func (spm *SuperPositionManager) CalculateAdjustments(ctx context.Context, newPrice decimal.Decimal) ([]*pb.OrderAction, error) {
	if spm.isMarginLocked() {
		return nil, nil
	}

	if spm.strategy == nil {
		return nil, fmt.Errorf("strategy not set")
	}

	spm.mu.RLock()
	anchorPrice := spm.anchorPrice
	slots := spm.slots
	spm.mu.RUnlock()

	// 1. Prepare state for strategy (convert map to slice if needed, but GridStrategy wants levels)
	// We need to pass the current slots to the strategy.
	// Since IStrategy is generic, we pass what it expects.
	// For GridStrategy, it's []grid.GridLevel.

	levels := make([]grid.GridLevel, 0, len(slots))
	for _, s := range slots {
		s.Mu.RLock()
		levels = append(levels, grid.GridLevel{
			Price:          pbu.ToGoDecimal(s.Price),
			PositionStatus: s.PositionStatus,
			PositionQty:    pbu.ToGoDecimal(s.PositionQty),
			SlotStatus:     s.SlotStatus,
			OrderSide:      s.OrderSide,
			OrderPrice:     pbu.ToGoDecimal(s.OrderPrice),
			OrderID:        s.OrderId,
		})
		s.Mu.RUnlock()
	}

	atr := decimal.Zero
	volFactor := 0.0
	isRiskTriggered := false
	if spm.riskMonitor != nil {
		atr = spm.riskMonitor.GetATR(spm.symbol)
		volFactor = spm.riskMonitor.GetVolatilityFactor(spm.symbol)
		isRiskTriggered = spm.riskMonitor.IsTriggered()
	}

	isCircuitTripped := false
	if spm.circuitBreaker != nil {
		isCircuitTripped = spm.circuitBreaker.IsTripped()
	}

	target, err := spm.strategy.CalculateTargetState(ctx, newPrice, anchorPrice, atr, volFactor, isRiskTriggered, isCircuitTripped, levels)
	if err != nil {
		return nil, err
	}

	// 2. Reconcile TargetState to Actions
	var actions []*pb.OrderAction

	// Index existing active orders by ClientOrderID
	activeOrders := make(map[string]*core.InventorySlot)
	for _, s := range slots {
		s.Mu.RLock()
		if s.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED && s.ClientOid != "" {
			activeOrders[s.ClientOid] = s
		}
		s.Mu.RUnlock()
	}

	desiredOids := make(map[string]bool)

	// Track slots to create/update
	spm.mu.Lock()
	for _, to := range target.Orders {
		desiredOids[to.ClientOrderID] = true

		if _, exists := activeOrders[to.ClientOrderID]; !exists {
			// Ensure slot exists for this price
			price := to.Price
			slot := spm.getOrCreateSlotLocked(price)

			// Proactively update slot state to PENDING
			slot.Mu.Lock()
			slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_PENDING
			slot.OrderPrice = pbu.FromGoDecimal(to.Price)
			slot.OrderSide = spm.mapSide(to.Side)
			slot.ClientOid = to.ClientOrderID
			spm.clientOMap[slot.ClientOid] = slot
			slot.Mu.Unlock()

			// PLACE missing order
			actions = append(actions, &pb.OrderAction{
				Type:  pb.OrderActionType_ORDER_ACTION_TYPE_PLACE,
				Price: pbu.FromGoDecimal(to.Price),
				Request: &pb.PlaceOrderRequest{
					Symbol:        to.Symbol,
					Side:          spm.mapSide(to.Side),
					Type:          pb.OrderType_ORDER_TYPE_LIMIT,
					Quantity:      pbu.FromGoDecimal(to.Quantity),
					Price:         pbu.FromGoDecimal(to.Price),
					ClientOrderId: to.ClientOrderID,
					ReduceOnly:    to.ReduceOnly,
					PostOnly:      to.PostOnly,
					TimeInForce:   pb.TimeInForce_TIME_IN_FORCE_GTC,
				},
			})
		}
	}
	spm.mu.Unlock()

	// Find orders to Cancel
	for oid, s := range activeOrders {
		if !desiredOids[oid] {
			s.Mu.RLock()
			actions = append(actions, &pb.OrderAction{
				Type:    pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL,
				Symbol:  spm.symbol,
				OrderId: s.OrderId,
				Price:   s.Price,
			})
			s.Mu.RUnlock()
		}
	}

	return actions, nil
}

func (spm *SuperPositionManager) mapSide(side string) pb.OrderSide {
	if side == "BUY" {
		return pb.OrderSide_ORDER_SIDE_BUY
	}
	return pb.OrderSide_ORDER_SIDE_SELL
}

// ApplyActionResults updates the internal state based on the results of executed actions
//
// LOCK ORDERING RULE: Always acquire locks in this order:
// 1. spm.mu (global lock)
// 2. slot.Mu (individual slot locks)
// NEVER acquire spm.mu while holding slot.Mu to prevent deadlocks
func (spm *SuperPositionManager) ApplyActionResults(results []core.OrderActionResult) error {
	// Phase 1: Collect slot updates while holding global lock
	type slotUpdate struct {
		slot     *core.InventorySlot
		result   core.OrderActionResult
		priceKey string
	}

	spm.mu.Lock()
	updates := make([]slotUpdate, 0, len(results))

	for _, res := range results {
		priceKey := pbu.ToGoDecimal(res.Action.Price).String()
		slot, exists := spm.slots[priceKey]
		if exists {
			updates = append(updates, slotUpdate{
				slot:     slot,
				result:   res,
				priceKey: priceKey,
			})
		}
	}
	spm.mu.Unlock()

	// Phase 2: Apply updates with per-slot locking
	for _, u := range updates {
		u.slot.Mu.Lock()

		if u.result.Error != nil {
			// Handle error case
			if u.result.Action.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE {
				if u.result.Action.Request != nil && u.slot.ClientOid == u.result.Action.Request.ClientOrderId {
					// Update slot state
					u.slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
					u.slot.OrderPrice = pbu.FromGoDecimal(decimal.Zero)
					clientOID := u.slot.ClientOid
					u.slot.ClientOid = ""
					u.slot.OrderSide = pb.OrderSide_ORDER_SIDE_UNSPECIFIED

					u.slot.Mu.Unlock()

					// Update maps with global lock
					spm.mu.Lock()
					delete(spm.clientOMap, clientOID)
					spm.mu.Unlock()
					continue
				}
			}
			u.slot.Mu.Unlock()
		} else if u.result.Action.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE && u.result.Order != nil {
			// Handle successful PLACE action
			u.slot.OrderId = u.result.Order.OrderId
			u.slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
			if u.result.Order.Status != pb.OrderStatus_ORDER_STATUS_UNSPECIFIED {
				u.slot.OrderStatus = u.result.Order.Status
			}
			u.slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED

			// Use order fields if present, otherwise preserve existing slot values
			if u.result.Order.Price != nil {
				u.slot.OrderPrice = u.result.Order.Price
			}
			if u.result.Order.Side != pb.OrderSide_ORDER_SIDE_UNSPECIFIED {
				u.slot.OrderSide = u.result.Order.Side
			}
			if u.result.Order.ClientOrderId != "" {
				u.slot.ClientOid = u.result.Order.ClientOrderId
			}

			orderID := u.result.Order.OrderId
			clientOrderID := u.slot.ClientOid
			side := u.slot.OrderSide
			orderType := u.result.Order.Type

			u.slot.Mu.Unlock()

			// Update maps with global lock
			spm.mu.Lock()
			spm.orderMap[orderID] = u.slot
			if clientOrderID != "" {
				spm.clientOMap[clientOrderID] = u.slot
			}
			spm.mu.Unlock()

			// Telemetry: Order placed
			telemetry.GetGlobalMetrics().OrdersPlacedTotal.Add(context.Background(), 1,
				metric.WithAttributes(
					attribute.String("symbol", spm.symbol),
					attribute.String("side", side.String()),
					attribute.String("type", orderType.String()),
				))

			// Record in order history
			spm.historyMu.Lock()
			spm.orderHistory = append(spm.orderHistory, u.result.Order)
			if len(spm.orderHistory) > 1000 {
				spm.orderHistory = spm.orderHistory[1:]
			}
			spm.historyMu.Unlock()
		} else if u.result.Action.Type == pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL {
			// Handle CANCEL action
			orderID := u.result.Action.OrderId
			clientOID := u.slot.ClientOid

			u.slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
			u.slot.OrderId = 0
			u.slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
			u.slot.OrderPrice = pbu.FromGoDecimal(decimal.Zero)
			u.slot.ClientOid = ""
			u.slot.OrderSide = pb.OrderSide_ORDER_SIDE_UNSPECIFIED

			u.slot.Mu.Unlock()

			// Update maps with global lock
			spm.mu.Lock()
			delete(spm.orderMap, orderID)
			if clientOID != "" {
				delete(spm.clientOMap, clientOID)
			}
			spm.mu.Unlock()
		} else {
			u.slot.Mu.Unlock()
		}
	}

	spm.mu.Lock()
	spm.updateTelemetry()
	spm.mu.Unlock()

	return nil
}

func (spm *SuperPositionManager) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	defer spm.updateTelemetry() // Update active orders count on update

	targetSlot := spm.orderMap[update.OrderId]
	if targetSlot == nil && update.ClientOrderId != "" {
		targetSlot = spm.clientOMap[update.ClientOrderId]
	}

	if targetSlot == nil {
		return fmt.Errorf("slot not found")
	}

	targetSlot.Mu.Lock()
	defer targetSlot.Mu.Unlock()

	switch update.Status {
	case pb.OrderStatus_ORDER_STATUS_FILLED:
		spm.handleOrderFilledLocked(targetSlot, update)
	case pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED:
		spm.handleOrderPartialFill(targetSlot, update)
	case pb.OrderStatus_ORDER_STATUS_CANCELED:
		spm.handleOrderCanceledLocked(targetSlot, update)
	case pb.OrderStatus_ORDER_STATUS_REJECTED:
		spm.handleOrderRejected(update)
	}

	return nil
}

func (spm *SuperPositionManager) CancelAllBuyOrders(ctx context.Context) ([]*pb.OrderAction, error) {
	spm.mu.RLock()
	defer spm.mu.RUnlock()

	var actions []*pb.OrderAction
	for _, slot := range spm.slots {
		slot.Mu.RLock()
		if slot.OrderSide == pb.OrderSide_ORDER_SIDE_BUY && slot.OrderId > 0 {
			actions = append(actions, &pb.OrderAction{
				Type: pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL, OrderId: slot.OrderId, Symbol: spm.symbol, Price: slot.Price,
			})
		}
		slot.Mu.RUnlock()
	}
	return actions, nil
}

func (spm *SuperPositionManager) CancelAllSellOrders(ctx context.Context) ([]*pb.OrderAction, error) {
	spm.mu.RLock()
	defer spm.mu.RUnlock()

	var actions []*pb.OrderAction
	for _, slot := range spm.slots {
		slot.Mu.RLock()
		if slot.OrderSide == pb.OrderSide_ORDER_SIDE_SELL && slot.OrderId > 0 {
			actions = append(actions, &pb.OrderAction{
				Type: pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL, OrderId: slot.OrderId, Symbol: spm.symbol, Price: slot.Price,
			})
		}
		slot.Mu.RUnlock()
	}
	return actions, nil
}

func (spm *SuperPositionManager) GetSlots() map[string]*core.InventorySlot {
	spm.mu.RLock()
	defer spm.mu.RUnlock()
	result := make(map[string]*core.InventorySlot)
	for k, v := range spm.slots {
		result[k] = v
	}
	return result
}

func (spm *SuperPositionManager) GetSlotCount() int {
	return int(atomic.LoadInt64(&spm.totalSlots))
}

func (spm *SuperPositionManager) GetSnapshot() *pb.PositionManagerSnapshot {
	spm.mu.RLock()
	defer spm.mu.RUnlock()

	slots := make(map[string]*pb.InventorySlot)
	for k, v := range spm.slots {
		// Deep copy the slot to avoid data races
		v.Mu.RLock()
		// Manual copy to avoid copying lock
		s := &pb.InventorySlot{
			Price:             v.InventorySlot.Price,
			PositionStatus:    v.InventorySlot.PositionStatus,
			PositionQty:       v.InventorySlot.PositionQty,
			OrderId:           v.InventorySlot.OrderId,
			ClientOid:         v.InventorySlot.ClientOid,
			OrderSide:         v.InventorySlot.OrderSide,
			OrderStatus:       v.InventorySlot.OrderStatus,
			OrderPrice:        v.InventorySlot.OrderPrice,
			OrderFilledQty:    v.InventorySlot.OrderFilledQty,
			SlotStatus:        v.InventorySlot.SlotStatus,
			PostOnlyFailCount: v.InventorySlot.PostOnlyFailCount,
		}
		v.Mu.RUnlock()
		slots[k] = s
	}

	return &pb.PositionManagerSnapshot{
		Symbol:         spm.symbol,
		Slots:          slots,
		AnchorPrice:    pbu.FromGoDecimal(spm.anchorPrice),
		TotalSlots:     atomic.LoadInt64(&spm.totalSlots),
		ActiveSlots:    atomic.LoadInt64(&spm.activeSlots),
		MarginLocked:   spm.isMarginLocked(),
		LastUpdateTime: time.Now().UnixNano(),
	}
}

func (spm *SuperPositionManager) CreateReconciliationSnapshot() map[string]*core.InventorySlot {
	spm.mu.RLock()
	defer spm.mu.RUnlock()

	snapshot := make(map[string]*core.InventorySlot, len(spm.slots))
	for k, v := range spm.slots {
		v.Mu.RLock()
		// Deep copy the protobuf data using proto.Clone to avoid copying locks
		pbCopy := proto.Clone(v.InventorySlot).(*pb.InventorySlot)
		v.Mu.RUnlock()

		snapshot[k] = &core.InventorySlot{
			InventorySlot: pbCopy,
		}
	}

	return snapshot
}

func (spm *SuperPositionManager) UpdateOrderIndex(orderID int64, clientOID string, slot *core.InventorySlot) {
	spm.mu.Lock()
	defer spm.mu.Unlock()
	if orderID != 0 {
		spm.orderMap[orderID] = slot
	}
	if clientOID != "" {
		spm.clientOMap[clientOID] = slot
	}
}

// getOrCreateSlotLocked retrieves or creates a slot (caller must hold spm.mu)
func (spm *SuperPositionManager) getOrCreateSlotLocked(price decimal.Decimal) *core.InventorySlot {
	roundedPrice := tradingutils.RoundPrice(price, spm.priceDecimals)
	key := roundedPrice.String()

	if slot, exists := spm.slots[key]; exists {
		return slot
	}

	slot := &core.InventorySlot{
		InventorySlot: &pb.InventorySlot{
			Price:          pbu.FromGoDecimal(roundedPrice),
			PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY,
			PositionQty:    pbu.FromGoDecimal(decimal.Zero),
			OrderStatus:    pb.OrderStatus_ORDER_STATUS_NEW,
			SlotStatus:     pb.SlotStatus_SLOT_STATUS_FREE,
			OrderSide:      pb.OrderSide_ORDER_SIDE_UNSPECIFIED,
		},
	}
	spm.slots[key] = slot
	atomic.AddInt64(&spm.totalSlots, 1)
	return slot
}

func (spm *SuperPositionManager) handleOrderFilledLocked(slot *core.InventorySlot, update *pb.OrderUpdate) {
	// IDEMPOTENCY CHECK #1: Check if slot is already in filled state with matching quantity
	// This handles the case where the same fill update is received multiple times
	if slot.OrderId == 0 &&
		slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_FREE &&
		pbu.ToGoDecimal(slot.OrderFilledQty).Equal(pbu.ToGoDecimal(update.ExecutedQty)) {
		// Slot already processed this fill - check position status matches expected state
		expectedPosStatus := pb.PositionStatus_POSITION_STATUS_FILLED
		if update.Side == pb.OrderSide_ORDER_SIDE_SELL {
			expectedPosStatus = pb.PositionStatus_POSITION_STATUS_EMPTY
		}

		if slot.PositionStatus == expectedPosStatus {
			spm.logger.Debug("Duplicate fill update ignored (slot already processed)",
				"order_id", update.OrderId,
				"qty", pbu.ToGoDecimal(update.ExecutedQty).String(),
				"side", update.Side.String())
			return
		}
	}

	// IDEMPOTENCY CHECK #2: Check global processed updates map
	// This catches duplicates even if slot state was modified between updates
	updateKey := fmt.Sprintf("%d-%s", update.OrderId, update.Status)
	spm.updateMu.Lock()
	if lastSeen, exists := spm.processedUpdates[updateKey]; exists {
		if time.Since(lastSeen) < 5*time.Minute {
			spm.updateMu.Unlock()
			spm.logger.Warn("Duplicate update detected via global tracking",
				"order_id", update.OrderId,
				"status", update.Status.String(),
				"last_seen", lastSeen,
				"age_seconds", time.Since(lastSeen).Seconds())
			return
		}
	}
	spm.processedUpdates[updateKey] = time.Now()
	spm.updateMu.Unlock()

	// Proceed with normal fill processing
	delete(spm.orderMap, update.OrderId)
	if update.ClientOrderId != "" {
		delete(spm.clientOMap, update.ClientOrderId)
	}
	if slot.OrderSide == pb.OrderSide_ORDER_SIDE_BUY {
		slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_FILLED
		slot.PositionQty = update.ExecutedQty
		slot.OrderFilledQty = update.ExecutedQty
	} else {
		slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_EMPTY
		slot.PositionQty = pbu.FromGoDecimal(decimal.Zero)
		slot.OrderFilledQty = update.ExecutedQty
	}
	slot.OrderId = 0
	slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
	slot.OrderPrice = pbu.FromGoDecimal(decimal.Zero)
	slot.ClientOid = ""
	slot.OrderSide = pb.OrderSide_ORDER_SIDE_UNSPECIFIED
	slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
	slot.PostOnlyFailCount = 0

	// Update telemetry
	telemetry.GetGlobalMetrics().OrdersFilledTotal.Add(context.Background(), 1, metric.WithAttributes(attribute.String("symbol", spm.symbol), attribute.String("side", update.Side.String())))
	spm.updateTelemetry()

	// Record Fill and History
	spm.historyMu.Lock()
	fill := &pb.Fill{
		FillId:   fmt.Sprintf("%d-%d", update.OrderId, time.Now().UnixNano()),
		OrderId:  fmt.Sprintf("%d", update.OrderId),
		Symbol:   spm.symbol,
		Side:     update.Side.String(),
		Quantity: update.ExecutedQty,
		Price:    update.Price,
		FilledAt: time.Now().UnixNano(),
	}
	spm.fills = append(spm.fills, fill)
	if len(spm.fills) > 1000 {
		spm.fills = spm.fills[1:]
	}

	// Simple Realized PnL: In a real system, we'd have a proper FIFO/LIFO matching to calculate PnL here.
	// For now we just track the fill.

	// Take a position snapshot for history
	snapshot := &pb.PositionSnapshotData{
		Symbol:        spm.symbol,
		Quantity:      slot.PositionQty,
		EntryPrice:    slot.Price,
		MarketPrice:   pbu.FromGoDecimal(spm.anchorPrice),
		UnrealizedPnl: pbu.FromGoDecimal(decimal.Zero), // simplified
		Timestamp:     time.Now().UnixNano(),
	}
	spm.positionHistory = append(spm.positionHistory, snapshot)
	if len(spm.positionHistory) > 1000 {
		spm.positionHistory = spm.positionHistory[1:]
	}
	spm.historyMu.Unlock()

	// Notify listeners
	spm.notifyUpdate(&pb.PositionUpdate{
		Position: &pb.PositionData{
			Symbol:       spm.symbol,
			Exchange:     spm.exchangeName,
			Quantity:     pbu.FromGoDecimal(pbu.ToGoDecimal(slot.PositionQty)),
			CurrentPrice: pbu.FromGoDecimal(spm.anchorPrice),
		},
		UpdateType: "filled",
		TriggerOrder: &pb.Order{
			OrderId: update.OrderId,
			Symbol:  spm.symbol,
			Status:  update.Status,
		},
	})
}

func (spm *SuperPositionManager) updateTelemetry() {
	// Calculate total position size
	var totalPos decimal.Decimal
	activeOrders := int64(0)

	for _, slot := range spm.slots {
		if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			totalPos = totalPos.Add(pbu.ToGoDecimal(slot.PositionQty))
		}
		if slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED || slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_PENDING {
			activeOrders++
		}
	}

	telemetry.GetGlobalMetrics().SetPositionSize(spm.symbol, totalPos.InexactFloat64())
	telemetry.GetGlobalMetrics().SetActiveOrders(spm.symbol, activeOrders)
}

func (spm *SuperPositionManager) handleOrderPartialFill(slot *core.InventorySlot, update *pb.OrderUpdate) {
	// IDEMPOTENCY CHECK: For partial fills, check if we've already seen this exact quantity
	currentFilledQty := pbu.ToGoDecimal(slot.OrderFilledQty)
	updateFilledQty := pbu.ToGoDecimal(update.ExecutedQty)

	// If the quantity hasn't changed, this is a duplicate
	if currentFilledQty.Equal(updateFilledQty) {
		spm.logger.Debug("Duplicate partial fill update ignored (quantity unchanged)",
			"order_id", update.OrderId,
			"qty", updateFilledQty.String())
		return
	}

	// Validate that the new quantity is greater than the current (partial fills should accumulate)
	if updateFilledQty.LessThan(currentFilledQty) {
		spm.logger.Warn("Invalid partial fill update: new quantity less than current",
			"order_id", update.OrderId,
			"current_qty", currentFilledQty.String(),
			"update_qty", updateFilledQty.String())
		return
	}

	slot.OrderFilledQty = update.ExecutedQty
}

func (spm *SuperPositionManager) handleOrderCanceledLocked(slot *core.InventorySlot, update *pb.OrderUpdate) {
	// IDEMPOTENCY CHECK: If slot is already in canceled/free state, ignore duplicate
	if slot.OrderId == 0 &&
		slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_FREE &&
		slot.OrderStatus == pb.OrderStatus_ORDER_STATUS_NEW {
		spm.logger.Debug("Duplicate cancel update ignored (slot already free)",
			"order_id", update.OrderId)
		return
	}

	// Global duplicate tracking
	updateKey := fmt.Sprintf("%d-%s", update.OrderId, update.Status)
	spm.updateMu.Lock()
	if lastSeen, exists := spm.processedUpdates[updateKey]; exists {
		if time.Since(lastSeen) < 5*time.Minute {
			spm.updateMu.Unlock()
			spm.logger.Warn("Duplicate cancel update detected via global tracking",
				"order_id", update.OrderId,
				"last_seen", lastSeen)
			return
		}
	}
	spm.processedUpdates[updateKey] = time.Now()
	spm.updateMu.Unlock()

	delete(spm.orderMap, update.OrderId)
	if update.ClientOrderId != "" {
		delete(spm.clientOMap, update.ClientOrderId)
	}
	slot.OrderId = 0
	slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
	slot.OrderPrice = pbu.FromGoDecimal(decimal.Zero)
	slot.ClientOid = ""
	slot.OrderSide = pb.OrderSide_ORDER_SIDE_UNSPECIFIED
	slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
}

func (spm *SuperPositionManager) handleOrderRejected(update *pb.OrderUpdate) {}

func (spm *SuperPositionManager) isMarginLocked() bool {
	return time.Now().UnixNano() < atomic.LoadInt64(&spm.marginLockUntil)
}

// cleanupProcessedUpdates periodically removes old entries from the processedUpdates map
// to prevent unbounded memory growth. Runs in a background goroutine.
func (spm *SuperPositionManager) cleanupProcessedUpdates() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		spm.updateMu.Lock()
		now := time.Now()
		removed := 0

		// Remove entries older than 5 minutes
		for key, timestamp := range spm.processedUpdates {
			if now.Sub(timestamp) > 5*time.Minute {
				delete(spm.processedUpdates, key)
				removed++
			}
		}

		mapSize := len(spm.processedUpdates)
		spm.updateMu.Unlock()

		if removed > 0 {
			spm.logger.Debug("Cleaned up old processed updates",
				"removed", removed,
				"remaining", mapSize)
		}
	}
}

// ForceSync forces the local position state to match the exchange state
func (spm *SuperPositionManager) ForceSync(ctx context.Context, symbol string, exchangeSize decimal.Decimal) error {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	spm.logger.Info("Force syncing position",
		"symbol", symbol,
		"target_size", exchangeSize)

	// 1. Calculate current total filled quantity
	currentTotal := decimal.Zero

	for _, slot := range spm.slots {
		slot.Mu.Lock()
		if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			qty := pbu.ToGoDecimal(slot.PositionQty)
			currentTotal = currentTotal.Add(qty)
		}
		slot.Mu.Unlock()
	}

	// 2. Determine difference
	diff := exchangeSize.Sub(currentTotal)
	if diff.IsZero() {
		return nil
	}

	spm.logger.Info("Adjusting position", "difference", diff)

	// 3. Adjust Slots
	if diff.IsPositive() {
		// We need to INCREASE position size.
		remainingDiff := diff

		for _, slot := range spm.slots {
			if remainingDiff.LessThanOrEqual(decimal.Zero) {
				break
			}

			slot.Mu.Lock()

			// Try to find empty slots to fill
			if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_EMPTY {
				price := pbu.ToGoDecimal(slot.Price)
				if price.IsZero() {
					slot.Mu.Unlock()
					continue
				}

				// Standard qty for this slot
				slotQty := spm.orderQuantity.Div(price)

				fillQty := slotQty
				if remainingDiff.LessThan(slotQty) {
					fillQty = remainingDiff
				}

				slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_FILLED
				slot.PositionQty = pbu.FromGoDecimal(fillQty)
				slot.OrderFilledQty = pbu.FromGoDecimal(fillQty)
				slot.OrderSide = pb.OrderSide_ORDER_SIDE_SELL
				slot.OrderId = 0 // Synthetic

				remainingDiff = remainingDiff.Sub(fillQty)
			}
			slot.Mu.Unlock()
		}

		if remainingDiff.IsPositive() {
			spm.logger.Warn("ForceSync unable to fully match target (ran out of slots)", "remaining_diff", remainingDiff)
		}

	} else {
		// We need to DECREASE position size (diff is negative).
		toRemove := diff.Abs()

		for _, slot := range spm.slots {
			if toRemove.LessThanOrEqual(decimal.Zero) {
				break
			}

			slot.Mu.Lock()

			if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
				qty := pbu.ToGoDecimal(slot.PositionQty)

				if qty.LessThanOrEqual(toRemove) {
					// Fully clear this slot
					slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_EMPTY
					slot.PositionQty = pbu.FromGoDecimal(decimal.Zero)
					slot.OrderFilledQty = pbu.FromGoDecimal(decimal.Zero)
					slot.OrderSide = pb.OrderSide_ORDER_SIDE_BUY
					toRemove = toRemove.Sub(qty)
				} else {
					// Partially reduce this slot
					newQty := qty.Sub(toRemove)
					slot.PositionQty = pbu.FromGoDecimal(newQty)
					slot.OrderFilledQty = pbu.FromGoDecimal(newQty)
					toRemove = decimal.Zero
				}
			}
			slot.Mu.Unlock()
		}
	}

	return nil
}

func (spm *SuperPositionManager) OnUpdate(callback func(*pb.PositionUpdate)) {
	spm.callbackMu.Lock()
	defer spm.callbackMu.Unlock()
	spm.updateCallbacks = append(spm.updateCallbacks, callback)
}

func (spm *SuperPositionManager) notifyUpdate(update *pb.PositionUpdate) {
	spm.callbackMu.RLock()
	defer spm.callbackMu.RUnlock()

	for _, callback := range spm.updateCallbacks {
		cb := callback // Capture closure
		task := func() {
			cb(update)
		}

		if spm.broadcastPool != nil {
			// Submit to pool (non-blocking if configured)
			err := spm.broadcastPool.Submit(task)
			if err != nil {
				spm.logger.Warn("Failed to submit position update to pool", "error", err)
			}
		} else {
			// Fallback to goroutine
			go task()
		}
	}
}

func (spm *SuperPositionManager) GetFills() []*pb.Fill {
	spm.historyMu.RLock()
	defer spm.historyMu.RUnlock()
	return spm.fills
}

func (spm *SuperPositionManager) GetOrderHistory() []*pb.Order {
	spm.historyMu.RLock()
	defer spm.historyMu.RUnlock()
	return spm.orderHistory
}

func (spm *SuperPositionManager) GetPositionHistory() []*pb.PositionSnapshotData {
	spm.historyMu.RLock()
	defer spm.historyMu.RUnlock()
	return spm.positionHistory
}

func (spm *SuperPositionManager) GetRealizedPnL() decimal.Decimal {
	spm.historyMu.RLock()
	defer spm.historyMu.RUnlock()
	return spm.realizedPnL
}
