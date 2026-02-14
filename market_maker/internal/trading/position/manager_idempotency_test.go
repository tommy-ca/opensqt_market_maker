package position

import (
	"context"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"market_maker/pkg/telemetry"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
)

func init() {
	// Initialize telemetry for tests
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test")
	_ = telemetry.GetGlobalMetrics().InitMetrics(meter)
}

func TestOrderUpdateIdempotency_DuplicateFill(t *testing.T) {
	// Setup
	logger := &mockLogger{}
	spm := NewSuperPositionManager(
		"BTCUSDT",
		"test-exchange",
		100.0, // priceInterval
		0.001, // orderQuantity
		10.0,  // minOrderValue
		5,     // buyWindowSize
		5,     // sellWindowSize
		2,     // priceDecimals
		6,     // qtyDecimals
		nil,   // strategy
		nil,   // riskMonitor
		nil,   // circuitBreaker
		logger,
		nil, // workerPool
	)

	// Initialize with anchor price
	err := spm.Initialize(decimal.NewFromFloat(50000.0))
	require.NoError(t, err)

	// Create a test slot with an active BUY order
	testPrice := decimal.NewFromFloat(49900.0)
	slot := spm.getOrCreateSlotLocked(testPrice)
	slot.OrderId = 12345
	slot.ClientOid = "test-client-oid-1"
	slot.OrderSide = pb.OrderSide_ORDER_SIDE_BUY
	slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
	slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED
	slot.OrderPrice = pbu.FromGoDecimal(testPrice)

	// Register in order map
	spm.orderMap[12345] = slot
	spm.clientOMap["test-client-oid-1"] = slot

	// Create a fill update
	fillUpdate := &pb.OrderUpdate{
		OrderId:       12345,
		ClientOrderId: "test-client-oid-1",
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_BUY,
		Status:        pb.OrderStatus_ORDER_STATUS_FILLED,
		ExecutedQty:   pbu.FromGoDecimal(decimal.NewFromFloat(0.001)),
	}

	// First update - should process normally
	err = spm.OnOrderUpdate(context.Background(), fillUpdate)
	require.NoError(t, err)

	// Verify state after first update
	assert.Equal(t, pb.PositionStatus_POSITION_STATUS_FILLED, slot.PositionStatus)
	assert.Equal(t, "0.001", pbu.ToGoDecimal(slot.PositionQty).String())
	assert.Equal(t, int64(0), slot.OrderId)
	assert.Equal(t, pb.SlotStatus_SLOT_STATUS_FREE, slot.SlotStatus)

	// Re-register the slot in orderMap to simulate a duplicate message arriving
	// (in reality, the WebSocket might replay the message before our state was updated)
	spm.mu.Lock()
	spm.orderMap[12345] = slot
	spm.clientOMap["test-client-oid-1"] = slot
	spm.mu.Unlock()

	// Second update (duplicate) - should be ignored by idempotency check
	err = spm.OnOrderUpdate(context.Background(), fillUpdate)
	require.NoError(t, err)

	// Verify state unchanged (still FILLED with same quantity, not double-filled)
	assert.Equal(t, pb.PositionStatus_POSITION_STATUS_FILLED, slot.PositionStatus)
	assert.Equal(t, "0.001", pbu.ToGoDecimal(slot.PositionQty).String())
	assert.Equal(t, int64(0), slot.OrderId)
	assert.Equal(t, pb.SlotStatus_SLOT_STATUS_FREE, slot.SlotStatus)

	// Re-register again for third test
	spm.mu.Lock()
	spm.orderMap[12345] = slot
	spm.clientOMap["test-client-oid-1"] = slot
	spm.mu.Unlock()

	// Third update (duplicate after delay) - should still be ignored
	time.Sleep(100 * time.Millisecond)
	err = spm.OnOrderUpdate(context.Background(), fillUpdate)
	require.NoError(t, err)

	// Verify state still unchanged
	assert.Equal(t, pb.PositionStatus_POSITION_STATUS_FILLED, slot.PositionStatus)
	assert.Equal(t, "0.001", pbu.ToGoDecimal(slot.PositionQty).String())
}

func TestOrderUpdateIdempotency_DuplicateCancel(t *testing.T) {
	logger := &mockLogger{}
	spm := NewSuperPositionManager(
		"BTCUSDT",
		"test-exchange",
		100.0, 0.001, 10.0, 5, 5, 2, 6,
		nil, nil, nil, logger, nil,
	)

	err := spm.Initialize(decimal.NewFromFloat(50000.0))
	require.NoError(t, err)

	// Create a test slot with an active order
	testPrice := decimal.NewFromFloat(50100.0)
	slot := spm.getOrCreateSlotLocked(testPrice)
	slot.OrderId = 67890
	slot.ClientOid = "test-client-oid-2"
	slot.OrderSide = pb.OrderSide_ORDER_SIDE_SELL
	slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
	slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED

	spm.orderMap[67890] = slot
	spm.clientOMap["test-client-oid-2"] = slot

	// Create a cancel update
	cancelUpdate := &pb.OrderUpdate{
		OrderId:       67890,
		ClientOrderId: "test-client-oid-2",
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_SELL,
		Status:        pb.OrderStatus_ORDER_STATUS_CANCELED,
	}

	// First cancel - should process
	err = spm.OnOrderUpdate(context.Background(), cancelUpdate)
	require.NoError(t, err)
	assert.Equal(t, pb.SlotStatus_SLOT_STATUS_FREE, slot.SlotStatus)
	assert.Equal(t, int64(0), slot.OrderId)

	// Re-register for duplicate test
	spm.mu.Lock()
	spm.orderMap[67890] = slot
	spm.clientOMap["test-client-oid-2"] = slot
	spm.mu.Unlock()

	// Duplicate cancel - should be ignored
	err = spm.OnOrderUpdate(context.Background(), cancelUpdate)
	require.NoError(t, err)
	assert.Equal(t, pb.SlotStatus_SLOT_STATUS_FREE, slot.SlotStatus)
	assert.Equal(t, int64(0), slot.OrderId)
}

func TestOrderUpdateIdempotency_DuplicatePartialFill(t *testing.T) {
	logger := &mockLogger{}
	spm := NewSuperPositionManager(
		"BTCUSDT",
		"test-exchange",
		100.0, 0.002, 10.0, 5, 5, 2, 6,
		nil, nil, nil, logger, nil,
	)

	err := spm.Initialize(decimal.NewFromFloat(50000.0))
	require.NoError(t, err)

	testPrice := decimal.NewFromFloat(50000.0)
	slot := spm.getOrCreateSlotLocked(testPrice)
	slot.OrderId = 11111
	slot.ClientOid = "test-client-oid-3"
	slot.OrderSide = pb.OrderSide_ORDER_SIDE_BUY
	slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
	slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED
	slot.OrderFilledQty = pbu.FromGoDecimal(decimal.Zero)

	spm.orderMap[11111] = slot
	spm.clientOMap["test-client-oid-3"] = slot

	// First partial fill - 0.001
	partialUpdate1 := &pb.OrderUpdate{
		OrderId:       11111,
		ClientOrderId: "test-client-oid-3",
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_BUY,
		Status:        pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED,
		ExecutedQty:   pbu.FromGoDecimal(decimal.NewFromFloat(0.001)),
	}

	err = spm.OnOrderUpdate(context.Background(), partialUpdate1)
	require.NoError(t, err)
	assert.Equal(t, "0.001", pbu.ToGoDecimal(slot.OrderFilledQty).String())

	// Duplicate partial fill - same quantity, should be ignored
	err = spm.OnOrderUpdate(context.Background(), partialUpdate1)
	require.NoError(t, err)
	assert.Equal(t, "0.001", pbu.ToGoDecimal(slot.OrderFilledQty).String())

	// Second partial fill - increased to 0.0015
	partialUpdate2 := &pb.OrderUpdate{
		OrderId:       11111,
		ClientOrderId: "test-client-oid-3",
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_BUY,
		Status:        pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED,
		ExecutedQty:   pbu.FromGoDecimal(decimal.NewFromFloat(0.0015)),
	}

	err = spm.OnOrderUpdate(context.Background(), partialUpdate2)
	require.NoError(t, err)
	assert.Equal(t, "0.0015", pbu.ToGoDecimal(slot.OrderFilledQty).String())

	// Invalid update - quantity decreased, should be rejected
	invalidUpdate := &pb.OrderUpdate{
		OrderId:       11111,
		ClientOrderId: "test-client-oid-3",
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_BUY,
		Status:        pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED,
		ExecutedQty:   pbu.FromGoDecimal(decimal.NewFromFloat(0.0005)),
	}

	err = spm.OnOrderUpdate(context.Background(), invalidUpdate)
	require.NoError(t, err)
	// Quantity should remain unchanged
	assert.Equal(t, "0.0015", pbu.ToGoDecimal(slot.OrderFilledQty).String())
}

func TestOrderUpdateIdempotency_GlobalTracking(t *testing.T) {
	logger := &mockLogger{}
	spm := NewSuperPositionManager(
		"BTCUSDT",
		"test-exchange",
		100.0, 0.001, 10.0, 5, 5, 2, 6,
		nil, nil, nil, logger, nil,
	)

	err := spm.Initialize(decimal.NewFromFloat(50000.0))
	require.NoError(t, err)

	testPrice := decimal.NewFromFloat(49900.0)
	slot := spm.getOrCreateSlotLocked(testPrice)
	slot.OrderId = 99999
	slot.ClientOid = "test-client-oid-4"
	slot.OrderSide = pb.OrderSide_ORDER_SIDE_BUY
	slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
	slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED

	spm.orderMap[99999] = slot
	spm.clientOMap["test-client-oid-4"] = slot

	fillUpdate := &pb.OrderUpdate{
		OrderId:       99999,
		ClientOrderId: "test-client-oid-4",
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_BUY,
		Status:        pb.OrderStatus_ORDER_STATUS_FILLED,
		ExecutedQty:   pbu.FromGoDecimal(decimal.NewFromFloat(0.001)),
	}

	// Process the fill
	err = spm.OnOrderUpdate(context.Background(), fillUpdate)
	require.NoError(t, err)

	// Verify the update is in the processedUpdates map
	updateKey := "99999-ORDER_STATUS_FILLED"
	spm.updateMu.RLock()
	_, exists := spm.processedUpdates[updateKey]
	spm.updateMu.RUnlock()
	assert.True(t, exists, "Update should be tracked in processedUpdates map")

	// Re-register for duplicate test
	spm.mu.Lock()
	spm.orderMap[99999] = slot
	spm.clientOMap["test-client-oid-4"] = slot
	spm.mu.Unlock()

	// Send duplicate - should be caught by global tracking
	err = spm.OnOrderUpdate(context.Background(), fillUpdate)
	require.NoError(t, err)

	// Position should still be correct (not double-filled)
	assert.Equal(t, pb.PositionStatus_POSITION_STATUS_FILLED, slot.PositionStatus)
	assert.Equal(t, "0.001", pbu.ToGoDecimal(slot.PositionQty).String())
}

func TestCleanupProcessedUpdates(t *testing.T) {
	logger := &mockLogger{}
	spm := NewSuperPositionManager(
		"BTCUSDT",
		"test-exchange",
		100.0, 0.001, 10.0, 5, 5, 2, 6,
		nil, nil, nil, logger, nil,
	)

	// Add some test entries with old timestamps
	spm.updateMu.Lock()
	spm.processedUpdates["old-1"] = time.Now().Add(-10 * time.Minute)
	spm.processedUpdates["old-2"] = time.Now().Add(-6 * time.Minute)
	spm.processedUpdates["recent-1"] = time.Now().Add(-2 * time.Minute)
	spm.processedUpdates["recent-2"] = time.Now()
	spm.updateMu.Unlock()

	// Manually trigger cleanup logic
	spm.updateMu.Lock()
	now := time.Now()
	for key, timestamp := range spm.processedUpdates {
		if now.Sub(timestamp) > 5*time.Minute {
			delete(spm.processedUpdates, key)
		}
	}
	spm.updateMu.Unlock()

	// Verify old entries removed, recent entries kept
	spm.updateMu.RLock()
	_, exists1 := spm.processedUpdates["old-1"]
	_, exists2 := spm.processedUpdates["old-2"]
	_, exists3 := spm.processedUpdates["recent-1"]
	_, exists4 := spm.processedUpdates["recent-2"]
	spm.updateMu.RUnlock()

	assert.False(t, exists1, "Old entry should be cleaned up")
	assert.False(t, exists2, "Old entry should be cleaned up")
	assert.True(t, exists3, "Recent entry should be kept")
	assert.True(t, exists4, "Recent entry should be kept")
}

func TestOrderUpdateIdempotency_SellOrderFill(t *testing.T) {
	logger := &mockLogger{}
	spm := NewSuperPositionManager(
		"BTCUSDT",
		"test-exchange",
		100.0, 0.001, 10.0, 5, 5, 2, 6,
		nil, nil, nil, logger, nil,
	)

	err := spm.Initialize(decimal.NewFromFloat(50000.0))
	require.NoError(t, err)

	// Create a sell slot with position
	testPrice := decimal.NewFromFloat(50100.0)
	slot := spm.getOrCreateSlotLocked(testPrice)
	slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_FILLED
	slot.PositionQty = pbu.FromGoDecimal(decimal.NewFromFloat(0.001))
	slot.OrderId = 22222
	slot.ClientOid = "test-sell-oid"
	slot.OrderSide = pb.OrderSide_ORDER_SIDE_SELL
	slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
	slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED

	spm.orderMap[22222] = slot
	spm.clientOMap["test-sell-oid"] = slot

	fillUpdate := &pb.OrderUpdate{
		OrderId:       22222,
		ClientOrderId: "test-sell-oid",
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_SELL,
		Status:        pb.OrderStatus_ORDER_STATUS_FILLED,
		ExecutedQty:   pbu.FromGoDecimal(decimal.NewFromFloat(0.001)),
	}

	// First fill - should clear position
	err = spm.OnOrderUpdate(context.Background(), fillUpdate)
	require.NoError(t, err)
	assert.Equal(t, pb.PositionStatus_POSITION_STATUS_EMPTY, slot.PositionStatus)
	assert.Equal(t, "0", pbu.ToGoDecimal(slot.PositionQty).String())
	assert.Equal(t, pb.SlotStatus_SLOT_STATUS_FREE, slot.SlotStatus)

	// Re-register for duplicate test
	spm.mu.Lock()
	spm.orderMap[22222] = slot
	spm.clientOMap["test-sell-oid"] = slot
	spm.mu.Unlock()

	// Duplicate fill - should be ignored
	err = spm.OnOrderUpdate(context.Background(), fillUpdate)
	require.NoError(t, err)
	assert.Equal(t, pb.PositionStatus_POSITION_STATUS_EMPTY, slot.PositionStatus)
	assert.Equal(t, "0", pbu.ToGoDecimal(slot.PositionQty).String())
}
