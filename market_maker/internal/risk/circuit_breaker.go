package risk

import (
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"market_maker/pkg/telemetry"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
)

type CircuitConfig struct {
	MaxConsecutiveLosses int
	MaxDrawdownAmount    decimal.Decimal
	MaxDrawdownPercent   decimal.Decimal
	CooldownPeriod       time.Duration
}

type CircuitBreaker struct {
	mu                sync.RWMutex
	state             CircuitState
	config            CircuitConfig
	consecutiveLosses int
	totalPnL          decimal.Decimal
	lastTripped       time.Time
}

func NewCircuitBreaker(config CircuitConfig) *CircuitBreaker {
	return &CircuitBreaker{
		state:  CircuitClosed,
		config: config,
	}
}

func (cb *CircuitBreaker) RecordTrade(pnl decimal.Decimal) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if pnl.IsNegative() {
		cb.consecutiveLosses++
	} else {
		cb.consecutiveLosses = 0
	}

	cb.totalPnL = cb.totalPnL.Add(pnl)

	cb.checkThresholds()
}

func (cb *CircuitBreaker) checkThresholds() {
	if cb.state == CircuitOpen {
		return
	}

	// Check consecutive losses
	if cb.config.MaxConsecutiveLosses > 0 && cb.consecutiveLosses >= cb.config.MaxConsecutiveLosses {
		cb.trip("Max consecutive losses reached")
		return
	}

	// Check absolute drawdown
	if !cb.config.MaxDrawdownAmount.IsZero() && cb.totalPnL.LessThan(cb.config.MaxDrawdownAmount.Neg()) {
		cb.trip("Max drawdown amount reached")
		return
	}
}

func (cb *CircuitBreaker) trip(reason string) {
	cb.state = CircuitOpen
	cb.lastTripped = time.Now()

	// Report metric
	telemetry.GetGlobalMetrics().SetCircuitBreakerOpen("global", true)
}

func (cb *CircuitBreaker) IsTripped() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitOpen {
		// Check for auto-reset if cooldown is configured
		if cb.config.CooldownPeriod > 0 && time.Since(cb.lastTripped) > cb.config.CooldownPeriod {
			cb.state = CircuitClosed
			cb.consecutiveLosses = 0
			cb.totalPnL = decimal.Zero
			return false
		}
		return true
	}
	return false
}

func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitClosed
	cb.consecutiveLosses = 0
	cb.totalPnL = decimal.Zero

	// Report metric
	telemetry.GetGlobalMetrics().SetCircuitBreakerOpen("global", false)
}

// Open manually trips the circuit breaker
func (cb *CircuitBreaker) Open(symbol string, reason string) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.trip(reason)
	return nil
}

func (cb *CircuitBreaker) GetStatus() *pb.CircuitBreakerStatus {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return &pb.CircuitBreakerStatus{
		IsOpen:            cb.state == CircuitOpen,
		ConsecutiveLosses: int32(cb.consecutiveLosses),
		TotalPnl:          pbu.FromGoDecimal(cb.totalPnL),
		OpenedAt:          cb.lastTripped.Unix(),
	}
}
