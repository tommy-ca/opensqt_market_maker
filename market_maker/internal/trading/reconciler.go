package trading

import (
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
)

// ReconcileResult contains the outcome of a reconciliation pass
type ReconcileResult struct {
	OrderMap       map[int64]*core.InventorySlot
	ClientOMap     map[string]*core.InventorySlot
	UnmatchedCount int
	ZombiesCleared int
}

// ReconcileOrders matches exchange open orders with local slots.
// It returns the updated order and clientOID maps, and a report of unmatched items.
func ReconcileOrders(
	logger core.ILogger,
	slots map[string]*core.InventorySlot,
	orders []*pb.Order,
) ReconcileResult {

	res := ReconcileResult{
		OrderMap:   make(map[int64]*core.InventorySlot),
		ClientOMap: make(map[string]*core.InventorySlot),
	}

	// 1. Identify which slots SHOULD be locked
	activePrices := make(map[string]*pb.Order)
	for _, o := range orders {
		activePrices[pbu.ToGoDecimal(o.Price).String()] = o
	}

	// 2. Reconcile all slots
	for priceKey, slot := range slots {
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
			slot.OrderSide = order.Side

			delete(activePrices, priceKey)
		} else {
			// No active order on exchange for this slot
			// If it was LOCKED or PENDING locally, it's now a "Zombie" and should be freed
			if slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED || slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_PENDING {
				logger.Warn("Clearing zombie slot during sync", "price", priceKey, "old_order_id", slot.OrderId)
				slot.OrderId = 0
				slot.ClientOid = ""
				slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_FREE
				slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
				res.ZombiesCleared++
			}
		}
		slot.Mu.Unlock()
	}

	res.UnmatchedCount = len(activePrices)
	for price, order := range activePrices {
		logger.Warn("Unmatched exchange order detected", "price", price, "order_id", order.OrderId)
	}

	return res
}
