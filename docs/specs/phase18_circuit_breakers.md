# Phase 18.1: Advanced Risk Controls - Circuit Breakers

**Project**: OpenSQT Market Maker  
**Phase**: 18.1 - Circuit Breakers  
**Status**: Active  
**Approach**: Specs-Driven Development + Test-Driven Development (TDD)

---

## 1. Requirement Specification

### 1.1 Overview
Circuit breakers are designed to protect capital by automatically pausing trading activities when certain risk thresholds are breached. This is critical for high-frequency trading where automated systems can generate large losses in seconds if they misbehave or if market conditions become extreme.

### 1.2 Functional Requirements

#### REQ-CB-001: Consecutive Loss Circuit Breaker
- The system MUST track the number of consecutive losing trades.
- If the count exceeds a configured threshold `MaxConsecutiveLosses`, the circuit breaker MUST trip.
- When tripped, all active orders for the symbol MUST be cancelled, and no new orders SHOULD be placed.

#### REQ-CB-002: Drawdown Circuit Breaker
- The system MUST monitor realized PnL within a sliding time window (e.g., 1 hour).
- If the drawdown exceeds `MaxDrawdownPercent` or `MaxDrawdownAmount`, the circuit breaker MUST trip.

#### REQ-CB-003: Latency Circuit Breaker
- The system MUST monitor the round-trip latency of gRPC requests to the `exchange_connector`.
- If the p99 latency exceeds `MaxLatencyMs` over a window of N requests, the circuit breaker MUST trip.

#### REQ-CB-004: Manual Override
- A user MUST be able to manually trip or reset the circuit breaker via a command or configuration update.

### 1.3 Circuit States
- **CLOSED**: Normal operation. Trading is active.
- **OPEN**: Circuit tripped. Trading is paused. Orders are cancelled.
- **HALF-OPEN**: (Optional) Testing mode after a cooldown period, allowing limited order placement to verify if conditions have stabilized.

---

## 2. Technical Design

### 2.1 Component Structure
A new `CircuitBreaker` component will be added to the `internal/risk` package.

```go
type CircuitBreaker struct {
    state         CircuitState
    config        CircuitConfig
    consecutiveLosses int
    windowPnL     decimal.Decimal
    lastTripped   time.Time
    // ...
}
```

### 2.2 Integration Points
- **PriceMonitor**: Provide real-time prices for drawdown calculation.
- **OrderExecutor**: Report trade results (fills and PnL) to the circuit breaker.
- **GridStrategy**: Query circuit breaker status before deciding to quote.

---

## 3. Test Plan (TDD)

### 3.1 Unit Tests
- `TestCircuitBreaker_ConsecutiveLoss`: Verify tripping after N losses.
- `TestCircuitBreaker_Drawdown`: Verify tripping after PnL drop.
- `TestCircuitBreaker_Reset`: Verify manual and automatic reset.

### 3.2 Integration Tests
- `TestGridStrategy_WithCircuitBreaker`: Verify strategy stops quoting when circuit is OPEN.

---

## 4. Acceptance Criteria
- ✅ Circuit trips correctly on all defined triggers.
- ✅ Orders are cancelled immediately upon tripping.
- ✅ Trading remains halted until manual reset or cooldown expiration.
- ✅ State transitions are logged with clear reasons.
