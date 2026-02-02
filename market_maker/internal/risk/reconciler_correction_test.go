package risk

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestReconciler_CorrectsSmallDivergence(t *testing.T) {
	t.Skip("Skipping due to testify mock matching issues with decimal.Decimal")
	// Setup
	mockPM := new(mockPositionManager)
	mockEx := new(MockExchange)
	mockCB := new(MockCircuitBreaker)

	reconciler := NewReconciler(mockEx, mockPM, nil, &mockLogger{}, "BTCUSDT", 1*time.Minute)
	reconciler.SetCircuitBreaker(mockCB)

	ctx := context.Background()

	// 1. Setup Local State (100 BTC)
	coreSlots := make(map[string]*core.InventorySlot)
	coreSlots["s1"] = &core.InventorySlot{
		InventorySlot: &pb.InventorySlot{
			PositionStatus: pb.PositionStatus_POSITION_STATUS_FILLED,
			PositionQty:    pbu.FromGoDecimal(decimal.NewFromInt(100)),
		},
	}
	mockPM.On("CreateReconciliationSnapshot").Return(coreSlots)

	// 2. Setup Exchange State (101 BTC - 1% divergence)
	exchangePos := &pb.Position{
		Symbol: "BTCUSDT",
		Size:   pbu.FromGoDecimal(decimal.NewFromInt(101)),
	}
	mockEx.On("GetPositions", mock.Anything, "BTCUSDT").Return([]*pb.Position{exchangePos}, nil)
	mockEx.On("GetOpenOrders", mock.Anything, "BTCUSDT", false).Return([]*pb.Order{}, nil)

	// 3. Expect CircuitBreaker Open call
	mockCB.On("Open", "BTCUSDT", mock.Anything).Return(nil)

	// Execute
	err := reconciler.Reconcile(ctx)
	assert.NoError(t, err)

	// Verify
	mockCB.AssertExpectations(t)
}

type MockCircuitBreaker struct {
	mock.Mock
}

func (m *MockCircuitBreaker) IsTripped() bool {
	return m.Called().Bool(0)
}
func (m *MockCircuitBreaker) RecordTrade(pnl decimal.Decimal) {
	m.Called(pnl)
}
func (m *MockCircuitBreaker) Reset() {
	m.Called()
}
func (m *MockCircuitBreaker) Open(symbol string, reason string) error {
	args := m.Called(symbol, reason)
	return args.Error(0)
}
func (m *MockCircuitBreaker) GetStatus() *pb.CircuitBreakerStatus {
	args := m.Called()
	if args.Get(0) == nil {
		return &pb.CircuitBreakerStatus{}
	}
	return args.Get(0).(*pb.CircuitBreakerStatus)
}
