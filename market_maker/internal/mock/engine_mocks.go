package mock

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MockOrderExecutor implements core.IOrderExecutor for testing
type MockOrderExecutor struct {
	orders     map[int64]*pb.Order
	orderCount int64
	mu         sync.Mutex
}

func NewMockOrderExecutor() *MockOrderExecutor {
	return &MockOrderExecutor{
		orders: make(map[int64]*pb.Order),
	}
}

func (m *MockOrderExecutor) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.orderCount++

	// Default to NEW status
	status := pb.OrderStatus_ORDER_STATUS_NEW
	executedQty := decimal.Zero

	// Instant fill for market orders in this mock
	if req.Type == pb.OrderType_ORDER_TYPE_MARKET {
		status = pb.OrderStatus_ORDER_STATUS_FILLED
		executedQty = pbu.ToGoDecimal(req.Quantity)
	}

	order := &pb.Order{
		OrderId:       m.orderCount,
		ClientOrderId: req.ClientOrderId,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Status:        status,
		Price:         req.Price,
		Quantity:      req.Quantity,
		ExecutedQty:   pbu.FromGoDecimal(executedQty),
		UpdateTime:    time.Now().UnixMilli(),
		CreatedAt:     timestamppb.Now(),
	}

	m.orders[order.OrderId] = order
	return order, nil
}

func (m *MockOrderExecutor) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	var placed []*pb.Order
	for _, req := range orders {
		o, _ := m.PlaceOrder(ctx, req)
		placed = append(placed, o)
	}
	return placed, true
}

func (m *MockOrderExecutor) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, id := range orderIDs {
		if o, ok := m.orders[id]; ok {
			o.Status = pb.OrderStatus_ORDER_STATUS_CANCELED
			o.UpdateTime = time.Now().UnixMilli()
		}
	}
	return nil
}

// MockPositionManager implements core.IPositionManager for testing
type MockPositionManager struct {
	slots map[string]*core.InventorySlot
	mu    sync.Mutex
}

func NewMockPositionManager() *MockPositionManager {
	return &MockPositionManager{
		slots: make(map[string]*core.InventorySlot),
	}
}

func (m *MockPositionManager) Initialize(anchorPrice decimal.Decimal) error {
	return nil
}

func (m *MockPositionManager) RestoreState(slots map[string]*pb.InventorySlot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range slots {
		m.slots[k] = &core.InventorySlot{InventorySlot: v}
	}
	return nil
}

func (m *MockPositionManager) CalculateAdjustments(ctx context.Context, newPrice decimal.Decimal) ([]*pb.OrderAction, error) {
	return nil, nil
}

func (m *MockPositionManager) ApplyActionResults(results []core.OrderActionResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, res := range results {
		if res.Action.Price == nil {
			continue
		}

		priceVal := pbu.ToGoDecimal(res.Action.Price)
		key := priceVal.String()

		slot, exists := m.slots[key]
		if !exists {
			// Don't create slot for cancel if it doesn't exist
			if res.Action.Type == pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL {
				continue
			}

			slot = &core.InventorySlot{
				InventorySlot: &pb.InventorySlot{
					Price:          res.Action.Price,
					SlotStatus:     pb.SlotStatus_SLOT_STATUS_FREE,
					PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY,
					PositionQty:    pbu.FromGoDecimal(decimal.Zero),
				},
			}
			m.slots[key] = slot
		}

		if res.Error != nil {
			slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
		} else if res.Action.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE && res.Order != nil {
			slot.OrderId = res.Order.OrderId
			slot.ClientOid = res.Order.ClientOrderId
			slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED
			slot.OrderStatus = res.Order.Status
			slot.OrderSide = res.Order.Side
			slot.OrderPrice = res.Order.Price
			slot.OrderFilledQty = res.Order.ExecutedQty
		} else if res.Action.Type == pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL {
			slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
			slot.OrderId = 0
			slot.ClientOid = ""
			slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_CANCELED
		}
	}
	return nil
}

func (m *MockPositionManager) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	return nil
}

func (m *MockPositionManager) CancelAllBuyOrders(ctx context.Context) ([]*pb.OrderAction, error) {
	return nil, nil
}

func (m *MockPositionManager) CancelAllSellOrders(ctx context.Context) ([]*pb.OrderAction, error) {
	return nil, nil
}

func (m *MockPositionManager) GetSlots() map[string]*core.InventorySlot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.slots
}

func (m *MockPositionManager) GetSlotCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.slots)
}

func (m *MockPositionManager) GetSnapshot() *pb.PositionManagerSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	pbSlots := make(map[string]*pb.InventorySlot)
	for k, v := range m.slots {
		pbSlots[k] = v.InventorySlot
	}
	return &pb.PositionManagerSnapshot{
		Slots: pbSlots,
	}
}

func (m *MockPositionManager) CreateReconciliationSnapshot() map[string]*core.InventorySlot {
	return m.GetSlots()
}

func (m *MockPositionManager) UpdateOrderIndex(orderID int64, clientOID string, slot *core.InventorySlot) {
}

func (m *MockPositionManager) ForceSync(ctx context.Context, symbol string, exchangeSize decimal.Decimal) error {
	return nil
}

func (m *MockPositionManager) RestoreFromExchangePosition(totalPosition decimal.Decimal) {
}

func (m *MockPositionManager) OnUpdate(callback func(*pb.PositionUpdate)) {
}

func (m *MockPositionManager) GetFills() []*pb.Fill {
	return nil
}

func (m *MockPositionManager) GetOrderHistory() []*pb.Order {
	return nil
}

func (m *MockPositionManager) GetPositionHistory() []*pb.PositionSnapshotData {
	return nil
}

func (m *MockPositionManager) GetRealizedPnL() decimal.Decimal {
	return decimal.Zero
}
