package position

import (
	"context"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/shopspring/decimal"
)

func TestSuperPositionManager_ForceSync(t *testing.T) {
	logger := &mockLogger{}
	rm := &mockRiskMonitor{}
	// Setup: 30 USDT per slot, Price 45000. Each slot ~0.00066667 BTC
	// OrderQty logic in SuperPositionManager usually takes FixedAmount (30.0) / Price
	pm := createTestPM("BTCUSDT", 1.0, 30.0, 5.0, 5, 5, 2, 8, rm, logger)

	anchorPrice := decimal.NewFromFloat(45000.0)
	_ = pm.Initialize(anchorPrice)

	// Pre-condition: 0 filled slots
	slots := pm.GetSlots()
	filledCount := 0
	for _, s := range slots {
		if s.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			filledCount++
		}
	}
	if filledCount != 0 {
		t.Fatalf("Expected 0 filled slots, got %d", filledCount)
	}

	// 1. Force Sync to 0.002 BTC (approx 3 slots worth: 3 * 30 / 45000 = 0.002)
	targetPos := decimal.NewFromFloat(0.002)
	err := pm.ForceSync(context.Background(), "BTCUSDT", targetPos)
	if err != nil {
		t.Fatalf("ForceSync failed: %v", err)
	}

	// Verify: Slots should be updated to reflect ~0.002 BTC
	slots = pm.GetSlots()
	filledCount = 0
	totalQty := decimal.Zero

	for _, s := range slots {
		if s.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			filledCount++
			qty := pbu.ToGoDecimal(s.PositionQty)
			totalQty = totalQty.Add(qty)

			// Verify side is SELL (since we are Long, we look to Sell)
			if s.OrderSide != pb.OrderSide_ORDER_SIDE_SELL {
				t.Errorf("Filled slot should be set to SELL side")
			}
		}
	}

	// 0.002 BTC / (30/45000) = 3 slots. Depending on exact prices picked (random map iteration),
	// it might take 3 or 4 slots.
	if filledCount < 3 || filledCount > 4 {
		t.Errorf("Expected 3-4 filled slots, got %d", filledCount)
	}

	// Allow small tolerance due to rounding
	if !totalQty.Sub(targetPos).Abs().LessThan(decimal.NewFromFloat(0.0001)) {
		t.Errorf("Expected total qty ~%s, got %s", targetPos, totalQty)
	}

	// 2. Force Sync down to 0 (Reset)
	err = pm.ForceSync(context.Background(), "BTCUSDT", decimal.Zero)
	if err != nil {
		t.Fatalf("ForceSync to zero failed: %v", err)
	}

	slots = pm.GetSlots()
	filledCount = 0
	for _, s := range slots {
		if s.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			filledCount++
		}
	}
	if filledCount != 0 {
		t.Errorf("Expected 0 filled slots after sync to zero, got %d", filledCount)
	}
}
