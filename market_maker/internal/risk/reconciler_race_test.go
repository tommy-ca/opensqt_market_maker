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
	"google.golang.org/protobuf/proto"
)

func TestReconciliationRaceCondition(t *testing.T) {
	pm := &mockPositionManager{
		slots: make(map[string]*core.InventorySlot),
	}

	slot := &core.InventorySlot{
		InventorySlot: &pb.InventorySlot{
			OrderId:     1,
			PositionQty: pbu.FromGoDecimal(decimal.NewFromInt(10)),
		},
	}
	pm.slots["key1"] = slot

	exchange := new(MockExchange)
	exchange.On("GetOpenOrders", mock.Anything, mock.Anything, mock.Anything).Return([]*pb.Order{}, nil)
	exchange.On("GetPositions", mock.Anything, mock.Anything).Return([]*pb.Position{
		{Symbol: "BTCUSDT", Size: pbu.FromGoDecimal(decimal.NewFromInt(10))},
	}, nil)
	pm.On("CreateReconciliationSnapshot").Return(func() map[string]*core.InventorySlot {
		// Simulate the deep copy behavior
		res := make(map[string]*core.InventorySlot)
		for k, v := range pm.slots {
			v.Mu.RLock()
			pbCopy := proto.Clone(v.InventorySlot).(*pb.InventorySlot)
			v.Mu.RUnlock()
			res[k] = &core.InventorySlot{InventorySlot: pbCopy}
		}
		return res
	}())

	reconciler := NewReconciler(exchange, pm, nil, &mockLogger{}, "BTCUSDT", 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				slot.Mu.Lock()
				slot.PositionQty = pbu.FromGoDecimal(decimal.NewFromInt(20))
				slot.OrderId = 2
				slot.Mu.Unlock()
				time.Sleep(time.Microsecond)
			}
		}
	}()

	for i := 0; i < 100; i++ {
		reconciler.Reconcile(ctx)
	}
}
