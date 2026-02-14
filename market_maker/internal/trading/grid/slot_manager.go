package grid

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/internal/trading"
	"market_maker/pkg/pbu"
	"sync"

	"github.com/shopspring/decimal"
)

// SlotManager handles the state of inventory slots and their mapping to orders
// It implements the core.IPositionManager interface.
type SlotManager struct {
	symbol        string
	priceDecimals int

	slots      map[int64]*core.InventorySlot
	orderMap   map[int64]*core.InventorySlot
	clientOMap map[string]*core.InventorySlot
	mu         sync.RWMutex

	// Stats (atomic)
	// totalSlots removed in favor of len(slots) under lock

	// Callbacks for services
	updateCallbacks []func(*pb.PositionUpdate)
	callbackMu      sync.RWMutex

	logger core.ILogger
}

func NewSlotManager(symbol string, priceDecimals int, logger core.ILogger) *SlotManager {
	return &SlotManager{
		symbol:        symbol,
		priceDecimals: priceDecimals,
		slots:         make(map[int64]*core.InventorySlot),
		orderMap:      make(map[int64]*core.InventorySlot),
		clientOMap:    make(map[string]*core.InventorySlot),
		logger:        logger.WithField("component", "slot_manager"),
	}
}

func (m *SlotManager) priceToInt64(price decimal.Decimal) int64 {
	return price.Mul(decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(m.priceDecimals)))).Round(0).IntPart()
}

func (m *SlotManager) Initialize(anchorPrice decimal.Decimal) error {
	return nil
}

func (m *SlotManager) GetAnchorPrice() decimal.Decimal {
	return decimal.Zero
}

// GetSlots returns a snapshot of all slots
func (m *SlotManager) GetSlots() map[string]*core.InventorySlot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make(map[string]*core.InventorySlot, len(m.slots))
	for _, v := range m.slots {
		res[v.PriceDec.String()] = v
	}
	return res
}

func (m *SlotManager) GetSlotCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.slots)
}

// SyncOrders updates the internal mappings for open orders
func (m *SlotManager) SyncOrders(orders []*pb.Order, exchangePosition decimal.Decimal) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := m.reconcileOrders(orders, exchangePosition)
	m.orderMap = result.OrderMap
	m.clientOMap = result.ClientOMap
}

func (m *SlotManager) reconcileOrders(orders []*pb.Order, exchangePosition decimal.Decimal) trading.ReconcileResult {
	res := trading.ReconcileResult{
		OrderMap:   make(map[int64]*core.InventorySlot),
		ClientOMap: make(map[string]*core.InventorySlot),
	}

	// 1. Identify which slots SHOULD be locked
	activePrices := make(map[int64]*pb.Order)
	for _, o := range orders {
		priceVal := pbu.ToGoDecimal(o.Price)
		activePrices[m.priceToInt64(priceVal)] = o
	}

	// 2. Calculate local filled position
	localFilled := decimal.Zero
	for _, slot := range m.slots {
		slot.Mu.RLock()
		if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			localFilled = localFilled.Add(slot.PositionQtyDec)
		}
		slot.Mu.RUnlock()
	}

	// 3. Reconcile all slots
	for priceKey, slot := range m.slots {
		slot.Mu.Lock()
		if order, ok := activePrices[priceKey]; ok {
			// Slot has an active order on exchange
			res.OrderMap[order.OrderId] = slot
			if order.ClientOrderId != "" {
				res.ClientOMap[order.ClientOrderId] = slot
			}

			slot.OrderId = order.OrderId
			slot.ClientOid = order.ClientOrderId
			slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED
			slot.OrderStatus = order.Status
			slot.OrderPrice = order.Price
			slot.OrderPriceDec = pbu.ToGoDecimal(order.Price)
			slot.OrderSide = order.Side

			delete(activePrices, priceKey)
		} else {
			// No active order on exchange for this slot
			// If it was LOCKED or PENDING locally, it might have been FILLED or CANCELED
			if slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED || slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_PENDING {
				// Check for Ghost Fills
				isGhostFill := false
				if slot.OrderSide == pb.OrderSide_ORDER_SIDE_BUY && exchangePosition.GreaterThan(localFilled) {
					m.logger.Warn("Adopting ghost BUY fill during sync", "price", slot.PriceDec, "order_id", slot.OrderId)
					slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_FILLED
					// We assume the full qty was filled for now
					slot.PositionQty = slot.OriginalQty
					slot.PositionQtyDec = slot.OriginalQtyDec
					localFilled = localFilled.Add(slot.PositionQtyDec)
					isGhostFill = true
				} else if slot.OrderSide == pb.OrderSide_ORDER_SIDE_SELL && exchangePosition.LessThan(localFilled) {
					m.logger.Warn("Adopting ghost SELL fill during sync", "price", slot.PriceDec, "order_id", slot.OrderId)
					slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_EMPTY
					localFilled = localFilled.Sub(slot.PositionQtyDec)
					slot.PositionQty = pbu.FromGoDecimal(decimal.Zero)
					slot.PositionQtyDec = decimal.Zero
					isGhostFill = true
				}

				if !isGhostFill {
					m.logger.Warn("Clearing zombie slot during sync", "price", slot.PriceDec, "old_order_id", slot.OrderId)
				}

				slot.OrderId = 0
				slot.ClientOid = ""
				slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
				slot.OrderPriceDec = decimal.Zero
				slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
				res.ZombiesCleared++
			}
		}
		slot.Mu.Unlock()
	}

	res.UnmatchedCount = len(activePrices)
	for price, order := range activePrices {
		m.logger.Warn("Unmatched exchange order detected", "price_key", price, "order_id", order.OrderId)
	}

	// Final drift check
	if !exchangePosition.Equal(localFilled) {
		m.logger.Error("CRITICAL: Position drift detected after reconciliation",
			"exchange", exchangePosition,
			"local", localFilled,
			"diff", exchangePosition.Sub(localFilled))
	}

	return res
}

// OnOrderUpdate handles an order execution report
func (m *SlotManager) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	slot := m.orderMap[update.OrderId]
	if slot == nil && update.ClientOrderId != "" {
		slot = m.clientOMap[update.ClientOrderId]
	}

	if slot == nil {
		return fmt.Errorf("slot not found for order %d", update.OrderId)
	}

	slot.Mu.Lock()
	defer slot.Mu.Unlock()

	// Apply status changes
	switch update.Status {
	case pb.OrderStatus_ORDER_STATUS_FILLED:
		m.handleFilled(slot, update)
	case pb.OrderStatus_ORDER_STATUS_CANCELED:
		m.handleCanceled(slot, update)
	}

	return nil
}

func (m *SlotManager) handleFilled(slot *core.InventorySlot, update *pb.OrderUpdate) {
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
	m.resetSlotLocked(slot)

	m.notifyUpdate(&pb.PositionUpdate{
		UpdateType: "filled",
		Position: &pb.PositionData{
			Symbol:   m.symbol,
			Quantity: slot.PositionQty,
		},
	})
}

func (m *SlotManager) handleCanceled(slot *core.InventorySlot, update *pb.OrderUpdate) {
	m.resetSlotLocked(slot)
}

func (m *SlotManager) resetSlotLocked(slot *core.InventorySlot) {
	// NOTE: m.mu and slot.Mu MUST be held by caller.
	// We follow the hierarchy: m.mu -> slot.Mu
	delete(m.orderMap, slot.OrderId)
	if slot.ClientOid != "" {
		delete(m.clientOMap, slot.ClientOid)
	}

	slot.OrderId = 0
	slot.ClientOid = ""
	slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
}

// GetOrCreateSlot ensures a slot exists for a given price
func (m *SlotManager) GetOrCreateSlot(price decimal.Decimal) *core.InventorySlot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getOrCreateSlotLocked(price)
}

func (m *SlotManager) getOrCreateSlotLocked(price decimal.Decimal) *core.InventorySlot {
	key := m.priceToInt64(price)
	if s, ok := m.slots[key]; ok {
		return s
	}

	s := &core.InventorySlot{
		InventorySlot: &pb.InventorySlot{
			Price:          pbu.FromGoDecimal(price),
			SlotStatus:     pb.SlotStatus_SLOT_STATUS_FREE,
			PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY,
			PositionQty:    pbu.FromGoDecimal(decimal.Zero),
			OriginalQty:    pbu.FromGoDecimal(decimal.Zero), // Should be set by caller if needed
		},
		PriceDec:          price,
		OrderPriceDec:     decimal.Zero,
		PositionQtyDec:    decimal.Zero,
		OriginalQtyDec:    decimal.Zero,
		OrderFilledQtyDec: decimal.Zero,
	}
	m.slots[key] = s
	return s
}

func (m *SlotManager) GetOrderIDForPrice(price decimal.Decimal) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := m.priceToInt64(price)
	if s, ok := m.slots[key]; ok {
		s.Mu.RLock()
		defer s.Mu.RUnlock()
		return s.OrderId
	}
	return 0
}

func (m *SlotManager) RestoreState(slots map[string]*pb.InventorySlot) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.slots = make(map[int64]*core.InventorySlot)
	m.orderMap = make(map[int64]*core.InventorySlot)
	m.clientOMap = make(map[string]*core.InventorySlot)

	for _, s := range slots {
		newSlot := &core.InventorySlot{
			InventorySlot:     s,
			PriceDec:          pbu.ToGoDecimal(s.Price),
			OrderPriceDec:     pbu.ToGoDecimal(s.OrderPrice),
			PositionQtyDec:    pbu.ToGoDecimal(s.PositionQty),
			OriginalQtyDec:    pbu.ToGoDecimal(s.OriginalQty),
			OrderFilledQtyDec: pbu.ToGoDecimal(s.OrderFilledQty),
		}
		key := m.priceToInt64(newSlot.PriceDec)
		m.slots[key] = newSlot
		if s.OrderId != 0 {
			m.orderMap[s.OrderId] = newSlot
		}
		if s.ClientOid != "" {
			m.clientOMap[s.ClientOid] = newSlot
		}
	}
	return nil
}

func (m *SlotManager) GetStrategySlots(target []core.StrategySlot) []core.StrategySlot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	num := len(m.slots)
	if cap(target) < num {
		target = make([]core.StrategySlot, num)
	} else {
		target = target[:num]
	}

	i := 0
	for _, s := range m.slots {
		s.Mu.RLock()
		target[i] = core.StrategySlot{
			Price:          s.PriceDec,
			PositionStatus: s.PositionStatus,
			PositionQty:    s.PositionQtyDec,
			SlotStatus:     s.SlotStatus,
			OrderSide:      s.OrderSide,
			OrderPrice:     s.OrderPriceDec,
			OrderId:        s.OrderId,
		}
		s.Mu.RUnlock()
		i++
	}
	return target
}

func (m *SlotManager) GetSnapshot() *pb.PositionManagerSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pbSlots := make(map[string]*pb.InventorySlot)
	for _, v := range m.slots {
		v.Mu.RLock()
		pbSlots[v.PriceDec.String()] = v.InventorySlot
		v.Mu.RUnlock()
	}
	return &pb.PositionManagerSnapshot{
		Symbol:     m.symbol,
		Slots:      pbSlots,
		TotalSlots: int64(len(m.slots)),
	}
}

func (m *SlotManager) ApplyActionResults(results []core.OrderActionResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, res := range results {
		priceVal := pbu.ToGoDecimal(res.Action.Price)
		slot := m.getOrCreateSlotLocked(priceVal)

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

			// Update manager maps while holding BOTH locks.
			// This is safe because we follow the hierarchy: m.mu -> slot.Mu
			m.orderMap[res.Order.OrderId] = slot
			if res.Order.ClientOrderId != "" {
				m.clientOMap[res.Order.ClientOrderId] = slot
			}
		}
		slot.Mu.Unlock()
	}
	return nil
}

func (m *SlotManager) CancelAllBuyOrders(ctx context.Context) ([]*pb.OrderAction, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var actions []*pb.OrderAction
	for _, slot := range m.slots {
		slot.Mu.RLock()
		if slot.OrderSide == pb.OrderSide_ORDER_SIDE_BUY && slot.OrderId != 0 {
			actions = append(actions, &pb.OrderAction{
				Type:    pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL,
				OrderId: slot.OrderId,
				Symbol:  m.symbol,
				Price:   slot.Price,
			})
		}
		slot.Mu.RUnlock()
	}
	return actions, nil
}

func (m *SlotManager) CancelAllSellOrders(ctx context.Context) ([]*pb.OrderAction, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var actions []*pb.OrderAction
	for _, slot := range m.slots {
		slot.Mu.RLock()
		if slot.OrderSide == pb.OrderSide_ORDER_SIDE_SELL && slot.OrderId != 0 {
			actions = append(actions, &pb.OrderAction{
				Type:    pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL,
				OrderId: slot.OrderId,
				Symbol:  m.symbol,
				Price:   slot.Price,
			})
		}
		slot.Mu.RUnlock()
	}
	return actions, nil
}

func (m *SlotManager) UpdateOrderIndex(orderID int64, clientOID string, slot *core.InventorySlot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if orderID != 0 {
		m.orderMap[orderID] = slot
	}
	if clientOID != "" {
		m.clientOMap[clientOID] = slot
	}
}

func (m *SlotManager) MarkSlotsPending(actions []*pb.OrderAction) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, action := range actions {
		priceVal := pbu.ToGoDecimal(action.Price)
		slot := m.getOrCreateSlotLocked(priceVal)

		slot.Mu.Lock()
		if action.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE {
			// Mark as PENDING if currently FREE
			if slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_FREE {
				slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_PENDING
				if action.Request != nil {
					slot.OrderPrice = action.Request.Price
					slot.OrderSide = action.Request.Side
					slot.ClientOid = action.Request.ClientOrderId
					if slot.ClientOid != "" {
						m.clientOMap[slot.ClientOid] = slot
					}
				}
			}
		} else if action.Type == pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL {
			// Mark as PENDING if currently LOCKED
			if slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED {
				slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_PENDING
			}
		}
		slot.Mu.Unlock()
	}
}

func (m *SlotManager) ForceSync(ctx context.Context, symbol string, exchangeSize decimal.Decimal) error {
	return nil
}

func (m *SlotManager) RestoreFromExchangePosition(totalPosition decimal.Decimal) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Restoring from exchange position", "total", totalPosition)

	// Simple implementation for Grid:
	// If totalPosition > 0, we assume it fills the slots starting from the highest buy price
	// or lowest sell price?
	// Actually, the most robust way is to look at existing filled slots and adjust.

	currentFilled := decimal.Zero
	for _, s := range m.slots {
		if s.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			currentFilled = currentFilled.Add(pbu.ToGoDecimal(s.PositionQty))
		}
	}

	if currentFilled.Equal(totalPosition) {
		return
	}

	m.logger.Warn("Position drift detected during boot", "local", currentFilled, "exchange", totalPosition)
	// For now, we don't automatically re-distribute unless we have the grid config here.
	// But we can at least log it.
}

func (m *SlotManager) OnUpdate(callback func(*pb.PositionUpdate)) {
	m.callbackMu.Lock()
	defer m.callbackMu.Unlock()
	m.updateCallbacks = append(m.updateCallbacks, callback)
}

func (m *SlotManager) notifyUpdate(update *pb.PositionUpdate) {
	m.callbackMu.RLock()
	defer m.callbackMu.RUnlock()
	for _, cb := range m.updateCallbacks {
		go cb(update)
	}
}

func (m *SlotManager) GetFills() []*pb.Fill {
	return nil
}

func (m *SlotManager) GetOrderHistory() []*pb.Order {
	return nil
}

func (m *SlotManager) GetPositionHistory() []*pb.PositionSnapshotData {
	return nil
}

func (m *SlotManager) GetRealizedPnL() decimal.Decimal {
	return decimal.Zero
}
