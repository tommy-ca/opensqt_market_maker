---
status: completed
priority: p2
issue_id: 012
tags: [code-review, agent-native, observability]
dependencies: []
---

# No Risk Control API - Agents Cannot Monitor or Control Risk Systems

## Problem Statement

The `RiskMonitor` and `CircuitBreaker` have **no programmatic interface**. Agents cannot query anomaly status, check trigger states, or understand why trading is halted. All state is internal-only with log-based visibility.

**Impact**:
- Agents cannot determine why trading stopped
- No way to query risk metrics programmatically
- Manual intervention required for all risk operations
- **Agent-native score: 0/7 risk capabilities accessible**

## Findings

**From Agent-Native Reviewer**:

**Location**: `internal/risk/monitor.go` and `internal/risk/circuit_breaker.go`

**Missing APIs**:
1. `RiskMonitor.GetStatus()` - Query anomaly detection state
2. `RiskMonitor.GetSymbolStats()` - Get per-symbol volume statistics
3. `RiskMonitor.Reset()` - Programmatically reset after investigation
4. `CircuitBreaker.GetStatus()` - Query breaker state (open/closed)
5. `CircuitBreaker.ManualTrip()` - Emergency stop capability
6. `CircuitBreaker.ManualReset()` - Resume after manual review
7. Export risk metrics via Prometheus

**Current State**: Resolved with gRPC RiskService.

## Proposed Solutions

### Option 1: Add gRPC RiskService (Recommended) - IMPLEMENTED
**Effort**: 1 day
**Risk**: Low
**Pros**:
- Consistent with existing gRPC architecture
- Type-safe protobuf API
- Easy to document and test

**Cons**:
- Need to add to existing server or create new service

**Implementation**:
```protobuf
// api/proto/opensqt/market_maker/v1/risk.proto
service RiskService {
  rpc GetRiskStatus(GetRiskStatusRequest) returns (RiskStatus);
  rpc GetSymbolStats(GetSymbolStatsRequest) returns (SymbolStats);
  rpc ResetRiskMonitor(ResetRequest) returns (ResetResponse);

  rpc GetCircuitBreakerStatus(GetCBStatusRequest) returns (CircuitBreakerStatus);
  rpc ManualTripCircuitBreaker(ManualTripRequest) returns (TripResponse);
  rpc ManualResetCircuitBreaker(ManualResetRequest) returns (ResetResponse);
}
```

## Recommended Action

**Option 1** (gRPC service) for consistency with exchange API.

## Technical Details

### Method Additions to RiskMonitor
```go
func (rm *RiskMonitor) IsTriggered() bool
func (rm *RiskMonitor) GetMetrics(symbol string) *pb.SymbolRiskMetrics
func (rm *RiskMonitor) Reset() error
func (rm *RiskMonitor) Subscribe(ch chan<- *pb.RiskAlert)
func (rm *RiskMonitor) Unsubscribe(ch chan<- *pb.RiskAlert)
```

### Method Additions to CircuitBreaker
```go
func (cb *CircuitBreaker) GetStatus() *pb.CircuitBreakerStatus
func (cb *CircuitBreaker) Open(symbol string, reason string) error
func (cb *CircuitBreaker) Reset()
```

### Integration Points
1. Add RiskService to gRPC server (Done)
2. Register in `cmd/market_maker/main.go` (Done)
3. Update health check to include risk status (Done)
4. Add Prometheus metrics export (Done)

## Acceptance Criteria

- [x] gRPC RiskService implemented
- [x] Agents can query risk monitor status
- [x] Agents can query circuit breaker state
- [x] Manual trip/reset endpoints work
- [x] Health check includes risk component status
- [x] Integration test verifies API accessibility
- [x] Documentation for agent API usage

## Work Log

**2026-01-22**: Agent-native review identified critical gap. Agents have 0% visibility into risk systems.
**2026-01-30**: Implemented full gRPC RiskService covering RiskMonitor, CircuitBreaker, and Reconciler. Added streaming alerts and Prometheus metrics.

## Resources

- Agent-Native Review: See agent output above
- gRPC Services: Similar to ExchangeService pattern
- Related: Issue #013 (Metrics export)
- Related: Issue #024 (Risk Monitoring API)

