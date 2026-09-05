package gridengine

import (
	"context"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// MockStore for testing
type MockStore struct {
	state *pb.State
}

func (m *MockStore) SaveState(ctx context.Context, state *pb.State) error {
	m.state = state
	return nil
}

func (m *MockStore) LoadState(ctx context.Context) (*pb.State, error) {
	return m.state, nil
}

func (m *MockStore) Close() error { return nil }

func TestGridEngine_OnPriceUpdate(t *testing.T) {
	// Setup Dependencies
	logger, _ := logging.NewZapLogger("INFO")
	mockExec := mock.NewMockOrderExecutor()
	mockSlotMgr := mock.NewMockPositionManager()
	mockStore := &MockStore{}

	// Create minimal config
	cfg := Config{
		Symbol:         "BTCUSDT",
		PriceInterval:  decimal.NewFromFloat(100.0),
		OrderQuantity:  decimal.NewFromFloat(0.001),
		MinOrderValue:  decimal.NewFromFloat(5.0),
		BuyWindowSize:  5,
		SellWindowSize: 5,
		PriceDecimals:  2,
		QtyDecimals:    3,
	}

	// Initialize Engine
	// Note: using nil risk monitor and exchanges for minimal unit test
	eng := NewGridEngine(
		nil, // exchanges
		mockExec,
		nil, // risk monitor
		mockStore,
		logger,
		nil, // worker pool
		mockSlotMgr,
		cfg,
	)

	ctx := context.Background()

	// Test OnPriceUpdate
	// This should trigger CalculateActions -> execute
	// MockPositionManager now updates its state via ApplyActionResults, so we can verify the saved state.

	price, _ := decimal.NewFromString("50000.00")
	update := &pb.PriceChange{
		Symbol: "BTCUSDT",
		Price:  pbu.FromGoDecimal(price),
	}

	err := eng.OnPriceUpdate(ctx, update)
	assert.NoError(t, err)

	// Verify State Saved
	assert.NotNil(t, mockStore.state)
	assert.Equal(t, "50000.00", pbu.ToGoDecimal(mockStore.state.LastPrice).StringFixed(2))

	// Verify Slots are populated
	assert.Greater(t, len(mockStore.state.Slots), 0, "Slots should be populated after initial update")
}

func TestGridEngine_Stop(t *testing.T) {
	// Setup Dependencies
	logger, _ := logging.NewZapLogger("INFO")
	mockExec := mock.NewMockOrderExecutor()
	mockSlotMgr := mock.NewMockPositionManager()
	mockStore := &MockStore{}

	// Create minimal config
	cfg := Config{
		Symbol: "BTCUSDT",
	}

	// Initialize Engine
	eng := NewGridEngine(
		nil, // exchanges
		mockExec,
		nil, // risk monitor
		mockStore,
		logger,
		nil, // worker pool
		mockSlotMgr,
		cfg,
	)

	// Test Stop
	err := eng.Stop()
	assert.NoError(t, err)
}
