package grid

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/internal/trading"
	"market_maker/pkg/pbu"
	"sync"
	"sync/atomic"

	"github.com/shopspring/decimal"
)

// SlotManager handles the state of inventory slots and their mapping to orders
// It implements the core.IPositionManager interface.
type SlotManager struct {
	symbol        string
	priceDecimals int

	slots      map[string]*core.InventorySlot
	orderMap   map[int64]*core.InventorySlot
	clientOMap map[string]*core.InventorySlot
	mu         sync.RWMutex

	// Stats (atomic)
	totalSlots int64

	// Callbacks for services
	updateCallbacks []func(*pb.PositionUpdate)
	callbackMu      sync.RWMutex

	logger core.ILogger
}

func NewSlotManager(symbol string, priceDecimals int, logger core.ILogger) *SlotManager {
	return &SlotManager{
		symbol:        symbol,
		priceDecimals: priceDecimals,
		slots:         make(map[string]*core.InventorySlot),
		orderMap:      make(map[int64]*core.InventorySlot),
		clientOMap:    make(map[string]*core.InventorySlot),
		logger:        logger.WithField("component", "slot_manager"),
	}
}

func (m *SlotManager) Initialize(anchorPrice decimal.Decimal) error {
	return nil
}

// GetSlots returns a snapshot of all slots
func (m *SlotManager) GetSlots() map[string]*core.InventorySlot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make(map[string]*core.InventorySlot)
	for k, v := range m.slots {
		res[k] = v
	}
	return res
}

func (m *SlotManager) GetSlotCount() int {
	return int(atomic.LoadInt64(&m.totalSlots))
}

// SyncOrders updates the internal mappings for open orders
func (m *SlotManager) SyncOrders(orders []*pb.Order, exchangePosition decimal.Decimal) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := trading.ReconcileOrders(m.logger, m.slots, orders, exchangePosition)
	m.orderMap = result.OrderMap
	m.clientOMap = result.ClientOMap
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
	} else {
		slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_EMPTY
		slot.PositionQty = pbu.FromGoDecimal(decimal.Zero)
		slot.PositionQtyDec = decimal.Zero
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
	key := price.String()
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
		PriceDec:       price,
		OrderPriceDec:  decimal.Zero,
		PositionQtyDec: decimal.Zero,
		OriginalQtyDec: decimal.Zero,
	}
	m.slots[key] = s
	atomic.AddInt64(&m.totalSlots, 1)
	return s
}

func (m *SlotManager) GetOrderIDForPrice(price decimal.Decimal) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := price.String()
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

	m.slots = make(map[string]*core.InventorySlot)
	m.orderMap = make(map[int64]*core.InventorySlot)
	m.clientOMap = make(map[string]*core.InventorySlot)

	for k, s := range slots {
		newSlot := &core.InventorySlot{
			InventorySlot:  s,
			PriceDec:       pbu.ToGoDecimal(s.Price),
			OrderPriceDec:  pbu.ToGoDecimal(s.OrderPrice),
			PositionQtyDec: pbu.ToGoDecimal(s.PositionQty),
			OriginalQtyDec: pbu.ToGoDecimal(s.OriginalQty),
		}
		m.slots[k] = newSlot
		if s.OrderId != 0 {
			m.orderMap[s.OrderId] = newSlot
		}
		if s.ClientOid != "" {
			m.clientOMap[s.ClientOid] = newSlot
		}
	}
	atomic.StoreInt64(&m.totalSlots, int64(len(slots)))
	return nil
}

func (m *SlotManager) GetSnapshot() *pb.PositionManagerSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pbSlots := make(map[string]*pb.InventorySlot)
	for k, v := range m.slots {
		v.Mu.RLock()
		pbSlots[k] = v.InventorySlot
		v.Mu.RUnlock()
	}
	return &pb.PositionManagerSnapshot{
		Symbol:     m.symbol,
		Slots:      pbSlots,
		TotalSlots: atomic.LoadInt64(&m.totalSlots),
	}
}

func (m *SlotManager) CalculateAdjustments(ctx context.Context, newPrice decimal.Decimal) ([]*pb.OrderAction, error) {
	return nil, nil
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
		// Both Places and Cancels should mark slots as PENDING to prevent double-execution
		// during rapid price updates.
		if action.Type != pb.OrderActionType_ORDER_ACTION_TYPE_PLACE &&
			action.Type != pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL {
			continue
		}
		priceVal := pbu.ToGoDecimal(action.Price)
		slot := m.getOrCreateSlotLocked(priceVal)

		slot.Mu.Lock()
		// Mark as PENDING if currently FREE (for PLACE) or LOCKED (for CANCEL)
		if slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_FREE ||
			slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED {
			slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_PENDING
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

func (m *SlotManager) CreateReconciliationSnapshot() map[string]*core.InventorySlot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make(map[string]*core.InventorySlot)
	for k, v := range m.slots {
		res[k] = v
	}
	return res
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
