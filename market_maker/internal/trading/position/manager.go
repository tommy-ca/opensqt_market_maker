package position

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/internal/trading"
	"market_maker/internal/trading/grid"
	"market_maker/pkg/pbu"
	"market_maker/pkg/tradingutils"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// SuperPositionManager manages multiple grid levels and their corresponding orders/positions
type SuperPositionManager struct {
	symbol       string
	exchangeName string

	// Config
	priceInterval  decimal.Decimal
	orderQuantity  decimal.Decimal
	minOrderValue  decimal.Decimal
	buyWindowSize  int
	sellWindowSize int
	priceDecimals  int
	qtyDecimals    int

	strategy *grid.Strategy
	monitor  core.IRiskMonitor
	store    core.IStateStore
	logger   core.ILogger

	// State tracking
	anchorPrice decimal.Decimal
	slots       map[string]*core.InventorySlot // Price -> Slot
	orderMap    map[int64]*core.InventorySlot  // OrderID -> Slot
	clientOMap  map[string]*core.InventorySlot // ClientOID -> Slot
	mu          sync.RWMutex

	// Stats
	totalSlots  int64
	activeSlots int64
	realizedPnL decimal.Decimal
	historyMu   sync.RWMutex

	// Idempotency
	processedUpdates map[string]time.Time
	updateMu         sync.RWMutex

	// Callbacks for services
	updateCallbacks []func(*pb.PositionUpdate)
	callbackMu      sync.RWMutex

	// Metrics
	meter metric.Meter
}

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
	strat *grid.Strategy,
	monitor core.IRiskMonitor,
	store core.IStateStore,
	logger core.ILogger,
	meter metric.Meter,
) *SuperPositionManager {
	spm := &SuperPositionManager{
		symbol:           symbol,
		exchangeName:     exchangeName,
		priceInterval:    decimal.NewFromFloat(priceInterval),
		orderQuantity:    decimal.NewFromFloat(orderQuantity),
		minOrderValue:    decimal.NewFromFloat(minOrderValue),
		buyWindowSize:    buyWindowSize,
		sellWindowSize:   sellWindowSize,
		priceDecimals:    priceDecimals,
		qtyDecimals:      qtyDecimals,
		strategy:         strat,
		monitor:          monitor,
		store:            store,
		logger:           logger.WithField("component", "position_manager").WithField("symbol", symbol),
		slots:            make(map[string]*core.InventorySlot),
		orderMap:         make(map[int64]*core.InventorySlot),
		clientOMap:       make(map[string]*core.InventorySlot),
		processedUpdates: make(map[string]time.Time),
		meter:            meter,
	}

	if meter != nil {
		spm.registerMetrics(meter, symbol)
	}

	return spm
}

func (spm *SuperPositionManager) registerMetrics(meter metric.Meter, symbol string) {
	// Register observable gauges
	commonAttrs := metric.WithAttributes(attribute.String("symbol", symbol))

	_, _ = meter.Int64ObservableGauge("position_total_slots",
		metric.WithDescription("Total number of grid slots"),
		metric.WithInt64Callback(func(_ context.Context, obs metric.Int64Observer) error {
			obs.Observe(atomic.LoadInt64(&spm.totalSlots), commonAttrs)
			return nil
		}))

	_, _ = meter.Int64ObservableGauge("position_active_slots",
		metric.WithDescription("Number of active grid slots (locked)"),
		metric.WithInt64Callback(func(_ context.Context, obs metric.Int64Observer) error {
			obs.Observe(atomic.LoadInt64(&spm.activeSlots), commonAttrs)
			return nil
		}))
}

func (spm *SuperPositionManager) Initialize(anchorPrice decimal.Decimal) error {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	spm.logger.Info("Initializing position manager",
		"anchor_price", anchorPrice.String(),
		"price_interval", spm.priceInterval.String(),
		"buy_window", spm.buyWindowSize,
		"sell_window", spm.sellWindowSize)

	spm.anchorPrice = anchorPrice

	// Pre-create slots
	buyPrices := tradingutils.CalculatePriceLevels(anchorPrice, spm.priceInterval.Neg(), spm.buyWindowSize)
	sellPrices := tradingutils.CalculatePriceLevels(anchorPrice, spm.priceInterval, spm.sellWindowSize)

	for _, price := range buyPrices {
		roundedPrice := tradingutils.RoundPrice(price, spm.priceDecimals)
		theoryQty := tradingutils.RoundQuantity(spm.orderQuantity.Div(price), spm.qtyDecimals)
		slot := &core.InventorySlot{
			InventorySlot: &pb.InventorySlot{
				Price:          pbu.FromGoDecimal(roundedPrice),
				PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY,
				PositionQty:    pbu.FromGoDecimal(decimal.Zero),
				OrderId:        0, ClientOid: "", OrderSide: pb.OrderSide_ORDER_SIDE_BUY,
				OrderStatus: pb.OrderStatus_ORDER_STATUS_NEW, OrderPrice: pbu.FromGoDecimal(decimal.Zero),
				OrderFilledQty: pbu.FromGoDecimal(decimal.Zero), SlotStatus: pb.SlotStatus_SLOT_STATUS_FREE,
				OriginalQty: pbu.FromGoDecimal(theoryQty),
			},
			PriceDec:          roundedPrice,
			OrderPriceDec:     decimal.Zero,
			PositionQtyDec:    decimal.Zero,
			OriginalQtyDec:    theoryQty,
			OrderFilledQtyDec: decimal.Zero,
		}
		spm.slots[roundedPrice.String()] = slot
		atomic.AddInt64(&spm.totalSlots, 1)
	}

	for _, price := range sellPrices {
		roundedPrice := tradingutils.RoundPrice(price, spm.priceDecimals)
		theoryQty := tradingutils.RoundQuantity(spm.orderQuantity.Div(price), spm.qtyDecimals)
		slot := &core.InventorySlot{
			InventorySlot: &pb.InventorySlot{
				Price:          pbu.FromGoDecimal(roundedPrice),
				PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY,
				PositionQty:    pbu.FromGoDecimal(decimal.Zero),
				OrderId:        0, ClientOid: "", OrderSide: pb.OrderSide_ORDER_SIDE_SELL,
				OrderStatus: pb.OrderStatus_ORDER_STATUS_NEW, OrderPrice: pbu.FromGoDecimal(decimal.Zero),
				OrderFilledQty: pbu.FromGoDecimal(decimal.Zero), SlotStatus: pb.SlotStatus_SLOT_STATUS_FREE,
				OriginalQty: pbu.FromGoDecimal(theoryQty),
			},
			PriceDec:          roundedPrice,
			OrderPriceDec:     decimal.Zero,
			PositionQtyDec:    decimal.Zero,
			OriginalQtyDec:    theoryQty,
			OrderFilledQtyDec: decimal.Zero,
		}
		spm.slots[roundedPrice.String()] = slot
		atomic.AddInt64(&spm.totalSlots, 1)
	}

	return nil
}

func (spm *SuperPositionManager) RestoreState(slots map[string]*pb.InventorySlot) error {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	spm.slots = make(map[string]*core.InventorySlot)
	spm.orderMap = make(map[int64]*core.InventorySlot)
	spm.clientOMap = make(map[string]*core.InventorySlot)

	var totalSlots int64
	for k, s := range slots {
		newSlot := &core.InventorySlot{
			InventorySlot:     s,
			PriceDec:          pbu.ToGoDecimal(s.Price),
			OrderPriceDec:     pbu.ToGoDecimal(s.OrderPrice),
			PositionQtyDec:    pbu.ToGoDecimal(s.PositionQty),
			OriginalQtyDec:    pbu.ToGoDecimal(s.OriginalQty),
			OrderFilledQtyDec: pbu.ToGoDecimal(s.OrderFilledQty),
		}
		spm.slots[k] = newSlot
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

		slot.Mu.Lock()
		slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_FILLED
		slot.PositionQty = pbu.FromGoDecimal(slotQty)
		slot.PositionQtyDec = slotQty

		slot.OrderId = 0
		slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
		slot.OrderSide = pb.OrderSide_ORDER_SIDE_SELL
		slot.ClientOid = ""
		slot.OrderFilledQty = pbu.FromGoDecimal(decimal.Zero)
		slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
		slot.Mu.Unlock()

		allocatedQty = allocatedQty.Add(slotQty)
	}
}

// CalculateAdjustments calculates required order adjustments by delegating to the strategy
func (spm *SuperPositionManager) CalculateAdjustments(ctx context.Context, newPrice decimal.Decimal) ([]*pb.OrderAction, error) {
	if spm.strategy == nil {
		return nil, fmt.Errorf("strategy not set")
	}

	// Use optimized slot collection
	stratSlots := spm.GetStrategySlots(nil)

	spm.mu.RLock()
	anchorPrice := spm.anchorPrice
	spm.mu.RUnlock()

	atr := decimal.Zero
	volFactor := 0.0
	isTriggered := false
	regime := pb.MarketRegime_MARKET_REGIME_RANGE

	if spm.monitor != nil {
		atr = spm.monitor.GetATR(spm.symbol)
		volFactor = spm.monitor.GetVolatilityFactor(spm.symbol)
		isTriggered = spm.monitor.IsTriggered()
		// SimpleEngine doesn't have RegimeMonitor for now, use RANGE
	}

	actions := spm.strategy.CalculateActions(newPrice, anchorPrice, atr, volFactor, isTriggered, regime, stratSlots)

	// Post-processing: Ensure slots exist for any PLACE actions returned by strategy
	spm.mu.Lock()
	defer spm.mu.Unlock()

	for _, action := range actions {
		if action.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE {
			price := pbu.ToGoDecimal(action.Price)
			slot := spm.getOrCreateSlotLocked(price)

			slot.Mu.Lock()
			if action.Request != nil {
				slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_PENDING
				slot.OrderPrice = action.Request.Price
				slot.OrderSide = action.Request.Side
				slot.ClientOid = action.Request.ClientOrderId
				spm.clientOMap[slot.ClientOid] = slot
			}
			slot.Mu.Unlock()
		}
	}

	return actions, nil
}

func (spm *SuperPositionManager) ApplyActionResults(results []core.OrderActionResult) error {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	for _, res := range results {
		priceKey := pbu.ToGoDecimal(res.Action.Price).String()
		slot, exists := spm.slots[priceKey]
		if !exists {
			continue
		}

		slot.Mu.Lock()
		if res.Error != nil {
			slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
		} else if res.Action.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE && res.Order != nil {
			slot.OrderId = res.Order.OrderId
			slot.ClientOid = res.Order.ClientOrderId
			slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED
			slot.OrderSide = res.Order.Side
			slot.OrderPrice = res.Order.Price
			slot.OrderPriceDec = pbu.ToGoDecimal(res.Order.Price)
			slot.OrderStatus = res.Order.Status
			slot.OriginalQty = res.Order.Quantity
			spm.orderMap[res.Order.OrderId] = slot
			if res.Order.ClientOrderId != "" {
				spm.clientOMap[res.Order.ClientOrderId] = slot
			}
		}
		slot.Mu.Unlock()
	}
	return nil
}

func (spm *SuperPositionManager) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	// 1. Global Idempotency Check
	// We skip this for partial fills because multiple updates with same status are expected
	if update.Status != pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED {
		updateKey := fmt.Sprintf("%d-%s", update.OrderId, update.Status.String())
		spm.updateMu.Lock()
		if lastSeen, exists := spm.processedUpdates[updateKey]; exists {
			if time.Since(lastSeen) < 5*time.Minute {
				spm.updateMu.Unlock()
				return nil
			}
		}
		spm.processedUpdates[updateKey] = time.Now()
		spm.updateMu.Unlock()
	}

	spm.mu.Lock()
	defer spm.mu.Unlock()

	slot, ok := spm.orderMap[update.OrderId]
	if !ok {
		slot, ok = spm.clientOMap[update.ClientOrderId]
	}

	if !ok {
		return nil
	}

	slot.Mu.Lock()
	defer slot.Mu.Unlock()

	// 2. Slot-level Idempotency Check for partial fills
	if update.Status == pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED {
		newExecuted := pbu.ToGoDecimal(update.ExecutedQty)
		oldExecuted := slot.OrderFilledQtyDec
		if newExecuted.LessThanOrEqual(oldExecuted) {
			return nil // Duplicate or stale partial fill
		}
		slot.OrderFilledQty = update.ExecutedQty
		slot.OrderFilledQtyDec = newExecuted
		return nil
	}

	if update.Status == pb.OrderStatus_ORDER_STATUS_FILLED {
		if slot.OrderSide == pb.OrderSide_ORDER_SIDE_BUY {
			slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_FILLED
			slot.PositionQty = update.ExecutedQty
			slot.PositionQtyDec = pbu.ToGoDecimal(update.ExecutedQty)
			slot.OrderFilledQtyDec = slot.PositionQtyDec
		} else {
			slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_EMPTY
			slot.PositionQty = pbu.FromGoDecimal(decimal.Zero)
			slot.PositionQtyDec = decimal.Zero
			slot.OrderFilledQtyDec = decimal.Zero
		}
		spm.resetSlotLocked(slot)

		spm.notifyUpdate(&pb.PositionUpdate{
			UpdateType: "filled",
			Position: &pb.PositionData{
				Symbol:   spm.symbol,
				Quantity: slot.PositionQty,
			},
		})
	} else if update.Status == pb.OrderStatus_ORDER_STATUS_CANCELED {
		spm.resetSlotLocked(slot)
	}

	return nil
}

func (spm *SuperPositionManager) resetSlotLocked(slot *core.InventorySlot) {
	// NOTE: spm.mu and slot.Mu MUST be held by caller.
	// We follow the hierarchy: Manager -> Slot
	delete(spm.orderMap, slot.OrderId)
	if slot.ClientOid != "" {
		delete(spm.clientOMap, slot.ClientOid)
	}

	slot.OrderId = 0
	slot.ClientOid = ""
	slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
}

func (spm *SuperPositionManager) GetSlots() map[string]*core.InventorySlot {
	spm.mu.RLock()
	defer spm.mu.RUnlock()

	// Return a copy of the map to avoid race conditions during iteration
	res := make(map[string]*core.InventorySlot, len(spm.slots))
	for k, v := range spm.slots {
		res[k] = v
	}
	return res
}

func (spm *SuperPositionManager) GetStrategySlots(target []core.StrategySlot) []core.StrategySlot {
	spm.mu.RLock()
	defer spm.mu.RUnlock()

	if cap(target) < len(spm.slots) {
		target = make([]core.StrategySlot, 0, len(spm.slots))
	} else {
		target = target[:0]
	}

	for _, s := range spm.slots {
		s.Mu.RLock()
		target = append(target, core.StrategySlot{
			Price:          s.PriceDec,
			PositionStatus: s.PositionStatus,
			PositionQty:    s.PositionQtyDec,
			SlotStatus:     s.SlotStatus,
			OrderSide:      s.OrderSide,
			OrderPrice:     s.OrderPriceDec,
			OrderId:        s.OrderId,
		})
		s.Mu.RUnlock()
	}
	return target
}

func (spm *SuperPositionManager) GetSnapshot() *pb.PositionManagerSnapshot {
	spm.mu.RLock()
	defer spm.mu.RUnlock()

	pbSlots := make(map[string]*pb.InventorySlot)
	for k, v := range spm.slots {
		v.Mu.RLock()
		pbSlots[k] = v.InventorySlot
		v.Mu.RUnlock()
	}

	return &pb.PositionManagerSnapshot{
		Symbol:     spm.symbol,
		Slots:      pbSlots,
		TotalSlots: atomic.LoadInt64(&spm.totalSlots),
	}
}

func (spm *SuperPositionManager) SyncOrders(orders []*pb.Order, exchangePosition decimal.Decimal) {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	result := trading.ReconcileOrders(spm.logger, spm.slots, orders, exchangePosition)
	spm.orderMap = result.OrderMap
	spm.clientOMap = result.ClientOMap
}

func (spm *SuperPositionManager) CancelAllBuyOrders(ctx context.Context) ([]*pb.OrderAction, error) {
	spm.mu.RLock()
	defer spm.mu.RUnlock()

	var actions []*pb.OrderAction
	for _, slot := range spm.slots {
		slot.Mu.RLock()
		if slot.OrderSide == pb.OrderSide_ORDER_SIDE_BUY && slot.OrderId != 0 {
			actions = append(actions, &pb.OrderAction{
				Type:    pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL,
				OrderId: slot.OrderId,
				Symbol:  spm.symbol,
				Price:   slot.Price,
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
		if slot.OrderSide == pb.OrderSide_ORDER_SIDE_SELL && slot.OrderId != 0 {
			actions = append(actions, &pb.OrderAction{
				Type:    pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL,
				OrderId: slot.OrderId,
				Symbol:  spm.symbol,
				Price:   slot.Price,
			})
		}
		slot.Mu.RUnlock()
	}
	return actions, nil
}

func (spm *SuperPositionManager) GetSlotCount() int {
	return int(atomic.LoadInt64(&spm.totalSlots))
}

func (spm *SuperPositionManager) CreateReconciliationSnapshot() map[string]*core.InventorySlot {
	spm.mu.RLock()
	defer spm.mu.RUnlock()

	snap := make(map[string]*core.InventorySlot)
	for k, v := range spm.slots {
		snap[k] = v
	}
	return snap
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

func (spm *SuperPositionManager) MarkSlotsPending(actions []*pb.OrderAction) {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	for _, action := range actions {
		if action.Type != pb.OrderActionType_ORDER_ACTION_TYPE_PLACE {
			continue
		}
		priceVal := pbu.ToGoDecimal(action.Price)
		slot := spm.getOrCreateSlotLocked(priceVal)

		slot.Mu.Lock()
		if slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_FREE {
			slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_PENDING
		}
		slot.Mu.Unlock()
	}
}

func (spm *SuperPositionManager) ForceSync(ctx context.Context, symbol string, exchangeSize decimal.Decimal) error {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	spm.logger.Warn("FORCE SYNC initiated", "exchange_size", exchangeSize)

	// 1. Calculate current local size
	localSize := decimal.Zero
	for _, s := range spm.slots {
		s.Mu.RLock()
		if s.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			localSize = localSize.Add(s.PositionQtyDec)
		}
		s.Mu.RUnlock()
	}

	if localSize.Equal(exchangeSize) {
		return nil
	}

	// 2. Adjust slots to match exchange size
	// This is a destructive operation - it will reset slot states to match reality.
	// For now, let's just use the RestoreFromExchangePosition logic.
	// But first, clear all filled slots.
	for _, s := range spm.slots {
		s.Mu.Lock()
		s.PositionStatus = pb.PositionStatus_POSITION_STATUS_EMPTY
		s.PositionQty = pbu.FromGoDecimal(decimal.Zero)
		s.PositionQtyDec = decimal.Zero
		s.Mu.Unlock()
	}

	spm.mu.Unlock() // RestoreFromExchangePosition will re-lock
	spm.RestoreFromExchangePosition(exchangeSize)
	spm.mu.Lock()

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
	for _, cb := range spm.updateCallbacks {
		go cb(update)
	}
}

func (spm *SuperPositionManager) GetFills() []*pb.Fill {
	return nil
}

func (spm *SuperPositionManager) GetOrderHistory() []*pb.Order {
	return nil
}

func (spm *SuperPositionManager) GetPositionHistory() []*pb.PositionSnapshotData {
	return nil
}

func (spm *SuperPositionManager) getOrCreateSlotLocked(price decimal.Decimal) *core.InventorySlot {
	key := price.String()
	if s, ok := spm.slots[key]; ok {
		return s
	}

	theoryQty := tradingutils.RoundQuantity(spm.orderQuantity.Div(price), spm.qtyDecimals)
	s := &core.InventorySlot{
		InventorySlot: &pb.InventorySlot{
			Price:          pbu.FromGoDecimal(price),
			SlotStatus:     pb.SlotStatus_SLOT_STATUS_FREE,
			PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY,
			PositionQty:    pbu.FromGoDecimal(decimal.Zero),
			OriginalQty:    pbu.FromGoDecimal(theoryQty),
		},
		PriceDec:       price,
		OrderPriceDec:  decimal.Zero,
		PositionQtyDec: decimal.Zero,
		OriginalQtyDec: theoryQty,
	}
	spm.slots[key] = s
	atomic.AddInt64(&spm.totalSlots, 1)
	return s
}

func (spm *SuperPositionManager) GetRealizedPnL() decimal.Decimal {
	spm.historyMu.RLock()
	defer spm.historyMu.RUnlock()
	return spm.realizedPnL
}
