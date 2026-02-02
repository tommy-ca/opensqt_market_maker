package simple

import (
	"context"
	"errors"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/logging"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockStore for testing persistence failures
type MockStore struct {
	mock.Mock
}

func (m *MockStore) SaveState(ctx context.Context, state *pb.State) error {
	args := m.Called(ctx, state)
	return args.Error(0)
}

func (m *MockStore) LoadState(ctx context.Context) (*pb.State, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.State), args.Error(1)
}

// MockPositionManagerForRollback
type MockPositionManagerForRollback struct {
	mock.Mock
}

func (m *MockPositionManagerForRollback) Initialize(anchorPrice decimal.Decimal) error { return nil }
func (m *MockPositionManagerForRollback) RestoreState(slots map[string]*pb.InventorySlot) error {
	args := m.Called(slots)
	return args.Error(0)
}
func (m *MockPositionManagerForRollback) CalculateAdjustments(ctx context.Context, newPrice decimal.Decimal) ([]*pb.OrderAction, error) {
	return nil, nil
}
func (m *MockPositionManagerForRollback) ApplyActionResults(results []core.OrderActionResult) error {
	return nil
}
func (m *MockPositionManagerForRollback) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	args := m.Called(ctx, update)
	return args.Error(0)
}
func (m *MockPositionManagerForRollback) CancelAllBuyOrders(ctx context.Context) ([]*pb.OrderAction, error) {
	return nil, nil
}
func (m *MockPositionManagerForRollback) CancelAllSellOrders(ctx context.Context) ([]*pb.OrderAction, error) {
	return nil, nil
}
func (m *MockPositionManagerForRollback) GetSlots() map[string]*core.InventorySlot {
	args := m.Called()
	return args.Get(0).(map[string]*core.InventorySlot)
}
func (m *MockPositionManagerForRollback) GetSlotCount() int { return 0 }
func (m *MockPositionManagerForRollback) GetSnapshot() *pb.PositionManagerSnapshot {
	args := m.Called()
	return args.Get(0).(*pb.PositionManagerSnapshot)
}
func (m *MockPositionManagerForRollback) CreateReconciliationSnapshot() map[string]*core.InventorySlot {
	return nil
}
func (m *MockPositionManagerForRollback) GetFills() []*pb.Fill                                      { return nil }
func (m *MockPositionManagerForRollback) GetOrderHistory() []*pb.Order                              { return nil }
func (m *MockPositionManagerForRollback) GetPositionHistory() []*pb.PositionSnapshotData            { return nil }
func (m *MockPositionManagerForRollback) GetRealizedPnL() decimal.Decimal                           { return decimal.Zero }
func (m *MockPositionManagerForRollback) RestoreFromExchangePosition(totalPosition decimal.Decimal) {}
func (m *MockPositionManagerForRollback) OnUpdate(callback func(*pb.PositionUpdate))                {}
func (m *MockPositionManagerForRollback) UpdateOrderIndex(orderID int64, clientOID string, slot *core.InventorySlot) {
}

func (m *MockPositionManagerForRollback) ForceSync(ctx context.Context, symbol string, exchangeSize decimal.Decimal) error {
	return nil
}

func TestOnOrderUpdate_PersistenceFailure_DoesNotMutateState(t *testing.T) {
	// Setup
	mockStore := new(MockStore)
	mockPM := new(MockPositionManagerForRollback)
	logger := logging.NewLogger(logging.InfoLevel, nil)

	engine := NewSimpleEngine(
		mockStore,
		mockPM,
		nil, // No order executor needed
		nil, // No risk monitor needed
		logger,
	)

	ctx := context.Background()
	update := &pb.OrderUpdate{
		OrderId: 123,
		Symbol:  "BTCUSDT",
		Status:  pb.OrderStatus_ORDER_STATUS_FILLED,
	}

	// 1. Expect GetSlots (for building preview state)
	initialSlots := map[string]*core.InventorySlot{
		"100": {
			InventorySlot: &pb.InventorySlot{
				OrderId:    123,
				SlotStatus: pb.SlotStatus_SLOT_STATUS_LOCKED,
			},
		},
	}
	mockPM.On("GetSlots").Return(initialSlots)

	// 2. Expect SaveState -> FAIL
	mockStore.On("SaveState", mock.Anything, mock.Anything).Return(errors.New("db failure"))

	// 3. We expect OnOrderUpdate NOT to be called on the mockPM
	// (Note: If it were called, the test would fail because we didn't set an expectation for it)

	// Execute
	err := engine.(*SimpleEngine).OnOrderUpdate(ctx, update)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "db failure")

	// Check that OnOrderUpdate was NOT called
	mockPM.AssertNotCalled(t, "OnOrderUpdate", mock.Anything, update)
	// Check that RestoreState was NOT called (no rollback needed anymore)
	mockPM.AssertNotCalled(t, "RestoreState", mock.Anything)
}
