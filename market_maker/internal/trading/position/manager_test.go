package position

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/internal/trading/grid"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func createTestPM(symbol string, interval, qty, minVal float64, buyW, sellW, pDec, qDec int, rm core.IRiskMonitor, logger core.ILogger) *SuperPositionManager {
	strat := grid.NewStrategy(grid.StrategyConfig{
		Symbol:         symbol,
		PriceInterval:  decimal.NewFromFloat(interval),
		OrderQuantity:  decimal.NewFromFloat(qty),
		MinOrderValue:  decimal.NewFromFloat(minVal),
		BuyWindowSize:  buyW,
		SellWindowSize: sellW,
		PriceDecimals:  pDec,
		QtyDecimals:    qDec,
		IsNeutral:      false,
	})
	return NewSuperPositionManager(symbol, "mock", interval, qty, minVal, buyW, sellW, pDec, qDec, strat, rm, nil, logger, nil)
}

func TestSuperPositionManager_RestoreFromExchangePosition(t *testing.T) {
	logger := &mockLogger{}
	rm := &mockRiskMonitor{}
	pm := createTestPM("BTCUSDT", 1.0, 30.0, 5.0, 5, 5, 2, 8, rm, logger)

	anchorPrice := decimal.NewFromFloat(45000.0)
	_ = pm.Initialize(anchorPrice)

	totalPos := decimal.NewFromFloat(0.002)
	pm.RestoreFromExchangePosition(totalPos)

	slots := pm.GetSlots()
	filledCount := 0
	sellSideCount := 0

	for _, slot := range slots {
		if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			filledCount++
			if slot.OrderSide == pb.OrderSide_ORDER_SIDE_SELL {
				sellSideCount++
			}
			price := pbu.ToGoDecimal(slot.Price)
			if price.LessThanOrEqual(anchorPrice) {
				t.Errorf("Filled slot price %s should be > anchor %s", price, anchorPrice)
			}
		}
	}

	if filledCount != 3 {
		t.Errorf("Expected 3 filled slots, got %d", filledCount)
	}
	if sellSideCount != 3 {
		t.Errorf("Expected 3 slots prepared for SELL, got %d", sellSideCount)
	}
}

func TestSuperPositionManager_CalculateAdjustments(t *testing.T) {
	logger := &mockLogger{}
	rm := &mockRiskMonitor{}
	pm := createTestPM("BTCUSDT", 1.0, 30.0, 5.0, 3, 3, 2, 3, rm, logger)

	_ = pm.Initialize(decimal.NewFromFloat(45000.0))
	actions, err := pm.CalculateAdjustments(context.Background(), decimal.NewFromFloat(44999.5))
	if err != nil {
		t.Fatalf("Failed to calculate adjustments: %v", err)
	}

	if len(actions) == 0 {
		t.Error("No actions were calculated")
	}

	foundPlace := false
	for _, a := range actions {
		if a.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE && a.Request.Side == pb.OrderSide_ORDER_SIDE_BUY {
			foundPlace = true
			break
		}
	}
	if !foundPlace {
		t.Error("Expected at least one PLACE action")
	}
}

func TestSuperPositionManager_ApplyActionResults(t *testing.T) {
	logger := &mockLogger{}
	rm := &mockRiskMonitor{}
	pm := createTestPM("BTCUSDT", 1.0, 30.0, 5.0, 2, 2, 2, 3, rm, logger)
	_ = pm.Initialize(decimal.NewFromFloat(45000.0))

	actions, _ := pm.CalculateAdjustments(context.Background(), decimal.NewFromFloat(44999.5))

	results := make([]core.OrderActionResult, len(actions))
	for i, a := range actions {
		results[i] = core.OrderActionResult{
			Action: a,
			Order:  &pb.Order{OrderId: int64(1000 + i)},
		}
	}

	err := pm.ApplyActionResults(results)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	slots := pm.GetSlots()
	lockedCount := 0
	for _, s := range slots {
		if s.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED {
			lockedCount++
		}
	}
	if lockedCount == 0 {
		t.Error("Expected some slots to be LOCKED")
	}
}

func TestSuperPositionManager_OnOrderUpdate(t *testing.T) {
	logger := &mockLogger{}
	rm := &mockRiskMonitor{}
	pm := createTestPM("BTCUSDT", 1.0, 30.0, 5.0, 2, 2, 2, 3, rm, logger)
	_ = pm.Initialize(decimal.NewFromFloat(45000.0))

	slots := pm.GetSlots()
	var testSlot *core.InventorySlot
	for _, slot := range slots {
		if slot.OrderSide == pb.OrderSide_ORDER_SIDE_BUY {
			testSlot = slot
			break
		}
	}

	testSlot.OrderId = 12345
	testSlot.ClientOid = "test_client_123"
	testSlot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
	testSlot.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED
	pm.UpdateOrderIndex(12345, "test_client_123", testSlot)

	update := pb.OrderUpdate{
		OrderId: 12345, ClientOrderId: "test_client_123", Symbol: "BTCUSDT",
		Status: pb.OrderStatus_ORDER_STATUS_FILLED,
		Price:  testSlot.Price, ExecutedQty: pbu.FromGoDecimal(decimal.NewFromFloat(30.0)),
		AvgPrice:   testSlot.Price,
		UpdateTime: time.Now().UnixMilli(),
	}

	err := pm.OnOrderUpdate(context.Background(), &update)
	if err != nil {
		t.Fatalf("Failed to handle update: %v", err)
	}
	if testSlot.PositionStatus != pb.PositionStatus_POSITION_STATUS_FILLED {
		t.Error("Expected FILLED position")
	}
}

func TestSuperPositionManager_RestoreState_RebuildsOrderMaps(t *testing.T) {
	logger := &mockLogger{}
	pm := NewSuperPositionManager(
		"BTCUSDT", "test", 100, 1, 10, 1, 1, 2, 3,
		nil, nil, nil, logger, nil,
	)

	slots := map[string]*pb.InventorySlot{
		"100.00": {
			Price:          pbu.FromGoDecimal(decimal.NewFromFloat(100.0)),
			OrderId:        111,
			ClientOid:      "cid-111",
			SlotStatus:     pb.SlotStatus_SLOT_STATUS_LOCKED,
			PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY,
		},
		"101.00": {
			Price:          pbu.FromGoDecimal(decimal.NewFromFloat(101.0)),
			OrderId:        222,
			ClientOid:      "cid-222",
			SlotStatus:     pb.SlotStatus_SLOT_STATUS_LOCKED,
			PositionStatus: pb.PositionStatus_POSITION_STATUS_EMPTY,
		},
	}

	if err := pm.RestoreState(slots); err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	pm.mu.RLock()
	_, ok1 := pm.orderMap[111]
	_, ok2 := pm.orderMap[222]
	_, okc1 := pm.clientOMap["cid-111"]
	_, okc2 := pm.clientOMap["cid-222"]
	pm.mu.RUnlock()

	if !ok1 || !ok2 || !okc1 || !okc2 {
		t.Fatalf("order/client maps not rebuilt correctly: orderMap(%v,%v) clientMap(%v,%v)", ok1, ok2, okc1, okc2)
	}
}

func TestSuperPositionManager_CancelAllBuyOrders(t *testing.T) {
	logger := &mockLogger{}
	rm := &mockRiskMonitor{}
	pm := createTestPM("BTCUSDT", 1.0, 30.0, 5.0, 2, 2, 2, 3, rm, logger)
	_ = pm.Initialize(decimal.NewFromFloat(45000.0))

	buySlots := 0
	for _, slot := range pm.GetSlots() {
		if slot.OrderSide == pb.OrderSide_ORDER_SIDE_BUY {
			slot.OrderId = int64(1000 + buySlots)
			slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
			slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED
			buySlots++
		}
	}

	actions, err := pm.CancelAllBuyOrders(context.Background())
	if err != nil {
		t.Fatalf("Failed to cancel: %v", err)
	}

	if len(actions) != buySlots {
		t.Errorf("Expected %d cancel actions, got %d", buySlots, len(actions))
	}
}

func TestSuperPositionManager_GetSlotCount(t *testing.T) {
	logger := &mockLogger{}
	rm := &mockRiskMonitor{}
	pm := createTestPM("BTCUSDT", 1.0, 30.0, 5.0, 3, 4, 2, 3, rm, logger)

	if pm.GetSlotCount() != 0 {
		t.Error("Expected 0")
	}
	_ = pm.Initialize(decimal.NewFromFloat(45000.0))
	if pm.GetSlotCount() != 7 {
		t.Errorf("Expected 7, got %d", pm.GetSlotCount())
	}
}

func TestSuperPositionManager_GetSnapshot(t *testing.T) {
	logger := &mockLogger{}
	rm := &mockRiskMonitor{}
	pm := createTestPM("BTCUSDT", 10.0, 100.0, 5.0, 2, 2, 2, 3, rm, logger)
	_ = pm.Initialize(decimal.NewFromFloat(45000.0))

	snapshot := pm.GetSnapshot()

	if snapshot.Symbol != "BTCUSDT" {
		t.Errorf("Expected symbol BTCUSDT, got %s", snapshot.Symbol)
	}
	// Logic: 2 buy + 2 sell = 4 slots
	if snapshot.TotalSlots != 4 {
		t.Errorf("Expected 4 slots, got %d", snapshot.TotalSlots)
	}
	if len(snapshot.Slots) != int(snapshot.TotalSlots) {
		t.Errorf("Expected %d slots in map, got %d", snapshot.TotalSlots, len(snapshot.Slots))
	}

	// Ensure we verify data integrity
	for _, slot := range snapshot.Slots {
		if slot.Price == nil {
			t.Error("Snapshot slot missing price")
		}
	}
}

func TestSuperPositionManager_DynamicGrid(t *testing.T) {
	logger := &mockLogger{}
	rm := &mockRiskMonitor{}
	pm := createTestPM("BTCUSDT", 10.0, 100.0, 5.0, 2, 2, 2, 3, rm, logger)

	_ = pm.Initialize(decimal.NewFromFloat(100.0))

	slots := pm.GetSlots()
	has90 := false
	if _, ok := slots["90"]; ok {
		has90 = true
	}
	if _, ok := slots["90.00"]; ok {
		has90 = true
	}

	if !has90 {
		t.Errorf("Expected slot at 90")
	}

	newPrice := decimal.NewFromFloat(200.0)
	actions, err := pm.CalculateAdjustments(context.Background(), newPrice)
	if err != nil {
		t.Fatalf("CalculateAdjustments failed: %v", err)
	}

	has190 := false
	for _, a := range actions {
		if a.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE {
			price := pbu.ToGoDecimal(a.Price)
			if price.Equal(decimal.NewFromFloat(190.0)) {
				has190 = true
			}
		}
	}

	if !has190 {
		t.Error("Dynamic Grid failed: Did not place order at new level 190.0")
	}

	currentSlots := pm.GetSlots()
	if len(currentSlots) != 6 {
		t.Errorf("Expected 6 slots, got %d", len(currentSlots))
	}
}

func BenchmarkCalculateAdjustments(b *testing.B) {
	logger := &mockLogger{}
	rm := &mockRiskMonitor{}
	pm := createTestPM("BTCUSDT", 1.0, 30.0, 5.0, 10, 10, 2, 3, rm, logger)
	_ = pm.Initialize(decimal.NewFromFloat(45000.0))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		price := decimal.NewFromFloat(45000.0 + float64(i%20))
		_, _ = pm.CalculateAdjustments(context.Background(), price)
	}
}
