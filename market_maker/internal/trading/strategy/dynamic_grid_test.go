package strategy

import (
	"context"
	"market_maker/internal/pb"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock RiskMonitor with ATR support
type MockRiskMonitorATR struct {
	mock.Mock
}

func (m *MockRiskMonitorATR) Start(ctx context.Context) error           { return nil }
func (m *MockRiskMonitorATR) Stop() error                               { return nil }
func (m *MockRiskMonitorATR) IsTriggered() bool                         { return false }
func (m *MockRiskMonitorATR) GetVolatilityFactor(symbol string) float64 { return 0 }
func (m *MockRiskMonitorATR) GetATR(symbol string) decimal.Decimal {
	args := m.Called(symbol)
	return args.Get(0).(decimal.Decimal)
}
func (m *MockRiskMonitorATR) GetAllSymbols() []string { return nil }
func (m *MockRiskMonitorATR) GetMetrics(symbol string) *pb.SymbolRiskMetrics {
	return nil
}
func (m *MockRiskMonitorATR) Reset() error { return nil }

func TestGridStrategy_DynamicInterval(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	rm := &MockRiskMonitorATR{}

	// Base Interval = 10.0
	strat := NewGridStrategy("BTCUSDT", "mock", decimal.NewFromFloat(10.0), decimal.NewFromFloat(1.0), decimal.NewFromFloat(5.0), 2, 2, 2, 3, false, rm, nil, logger)

	// Enable Dynamic Interval
	strat.SetDynamicInterval(true, 1.0) // Scale = 1.0

	// Case 1: Low Volatility (ATR = 5.0) -> Should use Base Interval (10.0)
	// Because Max(10, 5*1) = 10
	rm.On("GetATR", "BTCUSDT").Return(decimal.NewFromFloat(5.0)).Once()

	anchor := decimal.NewFromFloat(50000.0)
	current := decimal.NewFromFloat(49996.0) // Close to anchor (offset -4, interval 10 -> -0.4 -> 0 -> 50000)

	actions, _ := strat.CalculateActions(context.Background(), nil, anchor, current)

	// Expect Buy order at Anchor - Interval = 50000 - 10 = 49990

	found49990 := false
	for _, a := range actions {
		if a.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE {
			price := pbu.ToGoDecimal(a.Price)
			if price.Equal(decimal.NewFromFloat(49990.0)) {
				found49990 = true
			}
		}
	}
	assert.True(t, found49990, "Should find buy order at 49990 with base interval")

	// Case 2: High Volatility (ATR = 50.0) -> Should use ATR based Interval (50.0)
	// Max(10, 50*1) = 50
	rm.On("GetATR", "BTCUSDT").Return(decimal.NewFromFloat(50.0)).Once()

	actions, _ = strat.CalculateActions(context.Background(), nil, anchor, current)

	// Expect Buy order at Anchor - 50 = 49950
	found49950 := false
	found49990 = false
	for _, a := range actions {
		if a.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE {
			price := pbu.ToGoDecimal(a.Price)
			if price.Equal(decimal.NewFromFloat(49950.0)) {
				found49950 = true
			}
			if price.Equal(decimal.NewFromFloat(49990.0)) {
				found49990 = true
			}
		}
	}
	assert.True(t, found49950, "Should find buy order at 49950 with dynamic interval")
	assert.False(t, found49990, "Should NOT find buy order at 49990 (old interval)")
}
