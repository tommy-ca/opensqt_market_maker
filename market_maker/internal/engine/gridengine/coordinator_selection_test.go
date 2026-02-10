package gridengine

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/pkg/logging"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// MockGridExecutor for testing
type MockGridExecutor struct {
	ExecuteFunc func(ctx context.Context, actions []*pb.OrderAction)
}

func (m *MockGridExecutor) Execute(ctx context.Context, actions []*pb.OrderAction) {
	if m.ExecuteFunc != nil {
		m.ExecuteFunc(ctx, actions)
	}
}

// TestNewGridCoordinator_ExchangeSelection verifies the deterministic exchange selection logic
// ensuring stable behavior across restarts (resolves former TODO 035).
func TestNewGridCoordinator_ExchangeSelection(t *testing.T) {
	// Setup Dependencies
	logger, _ := logging.NewZapLogger("INFO")
	mockExec := &MockGridExecutor{}
	mockSlotMgr := mock.NewMockPositionManager()
	mockStore := &MockStore{}

	// Create mocks
	exchA := mock.NewMockExchange("ExchangeA")
	exchB := mock.NewMockExchange("ExchangeB")
	exchC := mock.NewMockExchange("ExchangeC")

	exchanges := map[string]core.IExchange{
		"ExchangeA": exchA,
		"ExchangeB": exchB,
		"ExchangeC": exchC,
	}

	baseCfg := Config{
		Symbol:        "BTCUSDT",
		OrderQuantity: decimal.NewFromFloat(0.001),
	}

	t.Run("Selects configured exchange when present", func(t *testing.T) {
		cfg := baseCfg
		cfg.Exchange = "ExchangeB"

		deps := GridCoordinatorDeps{
			Cfg:       cfg,
			Exchanges: exchanges,
			SlotMgr:   mockSlotMgr,
			Store:     mockStore,
			Logger:    logger,
			Executor:  mockExec,
		}

		coord := NewGridCoordinator(deps)

		assert.Equal(t, exchB, coord.exchange, "Should select configured ExchangeB")
	})

	t.Run("Falls back to deterministic selection (alphabetical) when config is empty", func(t *testing.T) {
		cfg := baseCfg
		cfg.Exchange = ""

		deps := GridCoordinatorDeps{
			Cfg:       cfg,
			Exchanges: exchanges,
			SlotMgr:   mockSlotMgr,
			Store:     mockStore,
			Logger:    logger,
			Executor:  mockExec,
		}

		coord := NewGridCoordinator(deps)

		// Should select ExchangeA because it is first alphabetically
		assert.Equal(t, exchA, coord.exchange, "Should select ExchangeA deterministically")
	})

	t.Run("Falls back to deterministic selection when configured exchange is missing", func(t *testing.T) {
		cfg := baseCfg
		cfg.Exchange = "NonExistent"

		deps := GridCoordinatorDeps{
			Cfg:       cfg,
			Exchanges: exchanges,
			SlotMgr:   mockSlotMgr,
			Store:     mockStore,
			Logger:    logger,
			Executor:  mockExec,
		}

		coord := NewGridCoordinator(deps)

		// Should fallback to ExchangeA
		assert.Equal(t, exchA, coord.exchange, "Should fallback to ExchangeA when configured is missing")
	})

	t.Run("Handles single exchange map correctly", func(t *testing.T) {
		cfg := baseCfg
		cfg.Exchange = ""

		singleExchanges := map[string]core.IExchange{
			"ExchangeZ": exchC,
		}

		deps := GridCoordinatorDeps{
			Cfg:       cfg,
			Exchanges: singleExchanges,
			SlotMgr:   mockSlotMgr,
			Store:     mockStore,
			Logger:    logger,
			Executor:  mockExec,
		}

		coord := NewGridCoordinator(deps)

		assert.Equal(t, exchC, coord.exchange, "Should select the only available exchange")
	})
}
