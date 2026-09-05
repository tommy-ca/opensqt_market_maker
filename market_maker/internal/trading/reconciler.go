package trading

import (
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"

	"github.com/shopspring/decimal"
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
	exchangePosition decimal.Decimal,
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

	// 2. Calculate local filled position
	localFilled := decimal.Zero
	for _, slot := range slots {
		slot.Mu.RLock()
		if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			localFilled = localFilled.Add(slot.PositionQtyDec)
		}
		slot.Mu.RUnlock()
	}

	// 3. Reconcile all slots
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
					logger.Warn("Adopting ghost BUY fill during sync", "price", priceKey, "order_id", slot.OrderId)
					slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_FILLED
					// We assume the full qty was filled for now
					slot.PositionQty = slot.OriginalQty
					slot.PositionQtyDec = slot.OriginalQtyDec
					localFilled = localFilled.Add(slot.PositionQtyDec)
					isGhostFill = true
				} else if slot.OrderSide == pb.OrderSide_ORDER_SIDE_SELL && exchangePosition.LessThan(localFilled) {
					logger.Warn("Adopting ghost SELL fill during sync", "price", priceKey, "order_id", slot.OrderId)
					slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_EMPTY
					localFilled = localFilled.Sub(slot.PositionQtyDec)
					slot.PositionQty = pbu.FromGoDecimal(decimal.Zero)
					slot.PositionQtyDec = decimal.Zero
					isGhostFill = true
				}

				if !isGhostFill {
					logger.Warn("Clearing zombie slot during sync", "price", priceKey, "old_order_id", slot.OrderId)
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
		logger.Warn("Unmatched exchange order detected", "price", price, "order_id", order.OrderId)
	}

	// Final drift check
	if !exchangePosition.Equal(localFilled) {
		logger.Error("CRITICAL: Position drift detected after reconciliation",
			"exchange", exchangePosition,
			"local", localFilled,
			"diff", exchangePosition.Sub(localFilled))
	}

	return res
}
