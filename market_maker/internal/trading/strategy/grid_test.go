package strategy

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"testing"

	"github.com/shopspring/decimal"
)

func TestGridStrategy_CalculateActions_Neutral(t *testing.T) {
	logger := &mockLogger{}
	strat := NewGridStrategy("BTCUSDT", "mock",
		decimal.NewFromFloat(10.0),  // interval
		decimal.NewFromFloat(100.0), // qty
		decimal.NewFromFloat(5.0),   // minVal
		2, 2,                        // buy/sell window
		2, 3, // decimals
		true, // isNeutral
		nil, nil, logger)

	anchorPrice := decimal.NewFromFloat(45000.0)
	currentPrice := decimal.NewFromFloat(45000.0)
	slots := make(map[string]*core.InventorySlot)

	// Test 1: Initial actions (should have 2 Buy and 2 Sell orders)
	actions, err := strat.CalculateActions(context.Background(), slots, anchorPrice, currentPrice)
	if err != nil {
		t.Fatalf("Failed to calculate: %v", err)
	}

	buyCount := 0
	sellCount := 0
	for _, a := range actions {
		if a.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE {
			if a.Request.Side == pb.OrderSide_ORDER_SIDE_BUY {
				buyCount++
			} else if a.Request.Side == pb.OrderSide_ORDER_SIDE_SELL {
				sellCount++
			}
		}
	}

	if buyCount != 2 || sellCount != 2 {
		t.Errorf("Expected 2 Buy and 2 Sell actions, got %d Buy and %d Sell", buyCount, sellCount)
	}
}

func TestGridStrategy_WithCircuitBreaker(t *testing.T) {
	logger := &mockLogger{}
	cb := &mockCircuitBreaker{tripped: true}
	strat := NewGridStrategy("BTCUSDT", "mock",
		decimal.NewFromFloat(10.0), decimal.NewFromFloat(100.0), decimal.NewFromFloat(5.0),
		2, 2, 2, 3, true, nil, cb, logger)

	actions, err := strat.CalculateActions(context.Background(), make(map[string]*core.InventorySlot), decimal.NewFromFloat(45000.0), decimal.NewFromFloat(45000.0))
	if err != nil {
		t.Fatal(err)
	}

	if len(actions) != 0 {
		t.Errorf("Expected 0 actions when circuit breaker is tripped, got %d", len(actions))
	}
}

type mockCircuitBreaker struct {
	tripped bool
}

func (m *mockCircuitBreaker) IsTripped() bool                 { return m.tripped }
func (m *mockCircuitBreaker) RecordTrade(pnl decimal.Decimal) {}
func (m *mockCircuitBreaker) Reset()                          {}
func (m *mockCircuitBreaker) Open(symbol string, reason string) error {
	m.tripped = true
	return nil
}
func (m *mockCircuitBreaker) GetStatus() *pb.CircuitBreakerStatus {
	return &pb.CircuitBreakerStatus{IsOpen: m.tripped}
}

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, f ...interface{})               {}
func (m *mockLogger) Info(msg string, f ...interface{})                {}
func (m *mockLogger) Warn(msg string, f ...interface{})                {}
func (m *mockLogger) Error(msg string, f ...interface{})               {}
func (m *mockLogger) Fatal(msg string, f ...interface{})               {}
func (m *mockLogger) WithField(k string, v interface{}) core.ILogger   { return m }
func (m *mockLogger) WithFields(f map[string]interface{}) core.ILogger { return m }
