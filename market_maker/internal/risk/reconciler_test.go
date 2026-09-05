package risk

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
)

func TestReconciler_ReconcileOrders(t *testing.T) {
	exchange := new(MockExchange)
	pm := &mockPositionManager{
		slots: make(map[string]*core.InventorySlot),
	}

	reconciler := NewReconciler(exchange, pm, nil, &mockLogger{}, "BTCUSDT", time.Minute)

	priceVal := decimal.NewFromFloat(45000.0)
	pricePb := pbu.FromGoDecimal(priceVal)

	slot := &core.InventorySlot{
		InventorySlot: &pb.InventorySlot{
			Price:      pricePb,
			OrderId:    12345,
			SlotStatus: pb.SlotStatus_SLOT_STATUS_LOCKED,
		},
	}
	pm.slots[priceVal.String()] = slot

	exchange.On("GetOpenOrders", mock.Anything, "BTCUSDT", false).Return([]*pb.Order{}, nil)
	exchange.On("GetPositions", mock.Anything, "BTCUSDT").Return([]*pb.Position{}, nil)

	pm.On("GetSlots").Return(map[string]*core.InventorySlot{
		priceVal.String(): slot,
	})
	pm.On("OnOrderUpdate", mock.Anything, mock.Anything).Return(nil)

	ctx := context.Background()
	err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconciliation failed: %v", err)
	}

	if len(pm.updates) != 1 {
		t.Errorf("Expected 1 update, got %d", len(pm.updates))
	} else {
		if pm.updates[0].Status != pb.OrderStatus_ORDER_STATUS_CANCELED {
			t.Errorf("Expected cancel update, got %s", pm.updates[0].Status)
		}
	}
}

func TestReconciler_GhostExchangeOrders(t *testing.T) {
	exchange := new(MockExchange)
	pm := &mockPositionManager{
		slots: make(map[string]*core.InventorySlot),
	}

	ctx := context.Background()
	req := &pb.PlaceOrderRequest{
		Symbol:   "BTCUSDT",
		Side:     pb.OrderSide_ORDER_SIDE_BUY,
		Price:    pbu.FromGoDecimal(decimal.NewFromFloat(44000.0)),
		Quantity: pbu.FromGoDecimal(decimal.NewFromFloat(0.1)),
	}

	ghostOrder := &pb.Order{OrderId: 999, Symbol: "BTCUSDT"}
	exchange.On("PlaceOrder", mock.Anything, req).Return(ghostOrder, nil)
	exchange.On("GetOpenOrders", mock.Anything, "BTCUSDT", false).Return([]*pb.Order{ghostOrder}, nil)
	exchange.On("GetPositions", mock.Anything, "BTCUSDT").Return([]*pb.Position{}, nil)
	exchange.On("CancelOrder", mock.Anything, "BTCUSDT", int64(999), false).Return(nil)
	pm.On("GetSlots").Return(make(map[string]*core.InventorySlot))

	_, _ = exchange.PlaceOrder(ctx, req)

	reconciler := NewReconciler(exchange, pm, nil, &mockLogger{}, "BTCUSDT", time.Minute)

	err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconciliation failed: %v", err)
	}

	exchange.AssertCalled(t, "CancelOrder", mock.Anything, "BTCUSDT", int64(999), false)
}
