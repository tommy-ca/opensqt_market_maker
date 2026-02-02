package position

import (
	"context"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	core "market_maker/internal/core"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/metadata"
)

func TestPositionServiceServer_GetPositions(t *testing.T) {
	// Setup
	logger := &mockLogger{}
	rm := &mockRiskMonitor{}
	// symbol="BTCUSDT", interval=1.0, qty=10.0, minVal=5.0, buyW=2, sellW=2, pDec=2, qDec=3
	pm := createTestPM("BTCUSDT", 1.0, 10.0, 5.0, 2, 2, 2, 3, rm, logger)
	pm.Initialize(decimal.NewFromFloat(50000.0))

	// Manually fill a slot to have some position
	slots := pm.GetSlots()
	filled := false
	var filledQty decimal.Decimal
	targetPrice := decimal.NewFromFloat(50001.0) // 50000 is anchor, first slot is at 50001 (sell) or 49999 (buy)

	for _, slot := range slots {
		if pbu.ToGoDecimal(slot.Price).Equal(targetPrice) {
			// This is a sell slot likely (or buy depending on exact logic), let's fill it
			slot.Mu.Lock()
			slot.PositionStatus = pb.PositionStatus_POSITION_STATUS_FILLED
			slot.PositionQty = pbu.FromGoDecimal(decimal.NewFromFloat(1.0))
			slot.Mu.Unlock()
			filled = true
			filledQty = decimal.NewFromFloat(1.0)
		}
	}
	assert.True(t, filled, "Failed to find slot at 50001.0")

	// Verify state before calling service
	snapshot := pm.GetSnapshot()
	foundInSnapshot := false
	for _, s := range snapshot.Slots {
		if s.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			foundInSnapshot = true
			break
		}
	}
	assert.True(t, foundInSnapshot, "Snapshot does not reflect filled slot")

	server := NewPositionServiceServer(pm, "binance")

	// Test GetPositions
	resp, err := server.GetPositions(context.Background(), &pb.PositionServiceGetPositionsRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Positions, 1)

	pos := resp.Positions[0]
	assert.Equal(t, "BTCUSDT", pos.Symbol)
	// We filled 1 slot with 1.0 qty
	assert.Equal(t, filledQty.String(), pos.Quantity.Value) // pbu string representation
}

func TestPositionServiceServer_GetOpenOrders(t *testing.T) {
	// Setup
	logger := &mockLogger{}
	rm := &mockRiskMonitor{}
	pm := createTestPM("BTCUSDT", 1.0, 10.0, 5.0, 2, 2, 2, 3, rm, logger)
	pm.Initialize(decimal.NewFromFloat(50000.0))

	// Lock a slot with an order
	slots := pm.GetSlots()
	for _, slot := range slots {
		slot.Mu.Lock()
		slot.SlotStatus = pb.SlotStatus_SLOT_STATUS_LOCKED
		slot.OrderId = 12345
		slot.OrderStatus = pb.OrderStatus_ORDER_STATUS_NEW
		slot.Mu.Unlock()
		break // just one
	}

	server := NewPositionServiceServer(pm, "binance")

	// Test
	resp, err := server.GetOpenOrders(context.Background(), &pb.PositionServiceGetOpenOrdersRequest{})
	assert.NoError(t, err)
	assert.Len(t, resp.Orders, 1)
	assert.Equal(t, int64(12345), resp.Orders[0].OrderId)
}

func TestPositionServiceServer_SubscribePositions(t *testing.T) {
	// Setup
	logger := &mockLogger{}
	rm := &mockRiskMonitor{}
	pm := createTestPM("BTCUSDT", 1.0, 10.0, 5.0, 2, 2, 2, 3, rm, logger)
	pm.Initialize(decimal.NewFromFloat(50000.0))

	server := NewPositionServiceServer(pm, "binance")

	// Mock stream
	stream := &mockPositionStream{
		ctx:     context.Background(),
		updates: make(chan *pb.PositionUpdate, 10),
	}

	// Start subscription in goroutine
	go func() {
		server.SubscribePositions(&pb.PositionServiceSubscribePositionsRequest{}, stream)
	}()

	// Give time for subscription to register
	time.Sleep(100 * time.Millisecond)

	// Trigger an update via PositionManager
	// We need to simulate OnOrderUpdate which triggers notification
	update := &pb.OrderUpdate{
		OrderId:       999,
		ClientOrderId: "test_oid",
		Status:        pb.OrderStatus_ORDER_STATUS_FILLED,
		ExecutedQty:   pbu.FromGoDecimal(decimal.NewFromFloat(1.0)),
		Side:          pb.OrderSide_ORDER_SIDE_BUY,
	}

	// We need a slot for this order to trigger the notify
	slots := pm.GetSlots()
	var targetSlot *core.InventorySlot
	for _, s := range slots {
		if s.OrderSide == pb.OrderSide_ORDER_SIDE_BUY {
			targetSlot = s
			break
		}
	}
	pm.UpdateOrderIndex(999, "test_oid", targetSlot)

	// This should trigger notifyUpdate
	pm.OnOrderUpdate(context.Background(), update)

	// Verify we got the update in the stream
	select {
	case received := <-stream.updates:
		assert.Equal(t, "filled", received.UpdateType)
		assert.Equal(t, "BTCUSDT", received.Position.Symbol)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for position update")
	}
}

// Mock Stream
type mockPositionStream struct {
	mock.Mock
	ctx     context.Context
	updates chan *pb.PositionUpdate
}

func (m *mockPositionStream) Send(u *pb.PositionUpdate) error {
	m.updates <- u
	return nil
}

func (m *mockPositionStream) Context() context.Context {
	return m.ctx
}

func (m *mockPositionStream) RecvMsg(m_ interface{}) error   { return nil }
func (m *mockPositionStream) SendMsg(m_ interface{}) error   { return nil }
func (m *mockPositionStream) SetHeader(h metadata.MD) error  { return nil }
func (m *mockPositionStream) SendHeader(h metadata.MD) error { return nil }
func (m *mockPositionStream) SetTrailer(h metadata.MD)       {}
