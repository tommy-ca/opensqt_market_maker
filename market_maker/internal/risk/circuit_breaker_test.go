package risk

import (
	"github.com/shopspring/decimal"
	"testing"
)

func TestCircuitBreaker_ConsecutiveLoss(t *testing.T) {
	config := CircuitConfig{
		MaxConsecutiveLosses: 3,
	}
	cb := NewCircuitBreaker(config)

	// Normal operation
	if cb.IsTripped() {
		t.Error("Circuit breaker should not be tripped initially")
	}

	// 1st loss
	cb.RecordTrade(decimal.NewFromFloat(-10.0))
	if cb.IsTripped() {
		t.Error("Circuit breaker should not trip after 1 loss")
	}

	// 1 win resets count
	cb.RecordTrade(decimal.NewFromFloat(5.0))
	if cb.consecutiveLosses != 0 {
		t.Errorf("Consecutive losses should be reset after a win, got %d", cb.consecutiveLosses)
	}

	// 3 consecutive losses
	cb.RecordTrade(decimal.NewFromFloat(-5.0))
	cb.RecordTrade(decimal.NewFromFloat(-5.0))
	cb.RecordTrade(decimal.NewFromFloat(-5.0))

	if !cb.IsTripped() {
		t.Error("Circuit breaker should trip after 3 consecutive losses")
	}
}

func TestCircuitBreaker_Drawdown(t *testing.T) {
	config := CircuitConfig{
		MaxDrawdownAmount: decimal.NewFromInt(100),
	}
	cb := NewCircuitBreaker(config)

	// Record a large loss
	cb.RecordTrade(decimal.NewFromInt(-150))

	if !cb.IsTripped() {
		t.Error("Circuit breaker should trip after exceeding max drawdown amount")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := CircuitConfig{
		MaxConsecutiveLosses: 1,
	}
	cb := NewCircuitBreaker(config)

	cb.RecordTrade(decimal.NewFromInt(-10))
	if !cb.IsTripped() {
		t.Fatal("Should be tripped")
	}

	cb.Reset()
	if cb.IsTripped() {
		t.Error("Should not be tripped after reset")
	}
	if cb.consecutiveLosses != 0 {
		t.Error("Consecutive losses should be 0 after reset")
	}
}
