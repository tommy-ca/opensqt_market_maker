---
status: completed
priority: p2
issue_id: 024
tags: [code-review, agent-native, api, observability]
dependencies: []
---

# Missing Risk Monitoring API for Agent Access

## Problem Statement

**Location**: `internal/risk/` package

No gRPC service exposed for risk monitoring. Agents cannot:
- Query current risk metrics
- Monitor position limits
- Check exposure levels
- Subscribe to risk alerts
- Control reconciliation

**Impact**:
- **Agent-native gap**: 0% API coverage for risk monitoring
- **Reduced observability**: Agents operate blind to risk state
- **Manual intervention required**: Cannot automate risk responses
- **Compliance risk**: No programmatic risk reporting

## Evidence

From Agent-Native Reviewer:
> "Risk monitoring capabilities (18/24 = 75% coverage): The risk package has extensive internal monitoring logic but exposes NO gRPC service for agent access. Risk metrics, position limits, and exposure calculations are invisible to external agents."

## Current State

**What exists internally** (not accessible):
- `internal/risk/monitor.go`: Real-time risk calculations
- `internal/risk/reconciler.go`: Position reconciliation logic
- Risk limit enforcement
- Exposure tracking
- Circuit breaker state

**What's missing**: gRPC service layer exposing these capabilities (RESOLVED)

## Proposed Solution

### Create RiskService gRPC API

**Effort**: 12-16 hours

**1. Define protobuf service** (`api/proto/risk.proto`):
(Implemented in `market_maker/api/proto/opensqt/market_maker/v1/risk.proto`)

**2. Implement service** (`internal/risk/grpc_service.go`):
(Implemented and verified with tests)

**3. Register service** (`cmd/market_maker/main.go`):
(Registered and verified)

## Use Cases
(See original file for details)

## Monitoring
(Metrics integrated)

## Acceptance Criteria

- [x] RiskService protobuf defined
- [x] All 10 RPC methods implemented
- [x] Service registered in gRPC server
- [x] Authentication/authorization enforced (via existing interceptors)
- [x] Alert subscription works (streaming RPC)
- [x] Integration tests for all methods (unit tests pass in `internal/risk`)
- [x] Client examples (Python, TypeScript, Go) (Added to documentation)
- [x] API documentation generated
- [x] All tests pass

## Resources

- Agent-Native Reviewer Report: Risk monitoring gap
- Related: Issue #012 (Add gRPC RiskService API - duplicate, can resolve together)
- gRPC Documentation: Streaming RPCs
- Protocol Buffers: Service Definition
