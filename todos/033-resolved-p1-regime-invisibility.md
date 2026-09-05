---
status: resolved
priority: p1
issue_id: "033"
tags: [code-review, agent-native, risk]
dependencies: []
---

# Regime Invisibility

The 'MarketRegime' status used by GridCoordinator for filtering is not exposed via gRPC. Agents cannot explain bot behavior without this context.

## Problem Statement

Lack of visibility into the internal filtering state. The 'MarketRegime' status used by GridCoordinator for filtering is not exposed via gRPC. Agents cannot explain bot behavior without this context.

## Findings

- Internal state `MarketRegime` is used within `GridCoordinator`.
- No gRPC endpoint exists to query this state.
- Agents (LLMs/Automated systems) lack the context necessary to explain why certain actions (or lack thereof) are occurring when filtering is active.

## Proposed Solutions

### Option 1: Add 'GetRegime' to 'RiskService'

**Approach:** Add a new RPC method `GetRegime` to the existing `RiskService` gRPC definition.

**Pros:**
- Centralizes risk-related state in one service.
- Easy for agents to discover.

**Cons:**
- RiskService might become a bottleneck if too many unrelated states are added.

**Effort:** 1-2 hours

**Risk:** Low

---

### Option 2: Add 'GetRegime' to 'PositionService'

**Approach:** Add the method to `PositionService` if regime is closely tied to position management logic.

**Pros:**
- Might fit better if positions are filtered based on regime.

**Cons:**
- Less intuitive than RiskService for "status" queries.

**Effort:** 1-2 hours

**Risk:** Low

## Recommended Action

Implemented Option 1: Added `GetRegime` to `RiskService`.

## Technical Details

**Affected files:**
- gRPC proto definitions
- `GridCoordinator` implementation
- Service implementations (RiskService or PositionService)

## Resources

- Issue ID: 033

## Acceptance Criteria

- [x] `GetRegime` added to gRPC service.
- [x] `GridCoordinator` exports the current regime state to the service.
- [x] Integration tests verify the regime can be queried via gRPC.

## Work Log

### 2026-02-10 - Initial Creation

**By:** Antigravity

**Actions:**
- Created todo file based on P1 finding.
- Identified potential gRPC services for the new endpoint.

### 2026-02-10 - Resolution

**By:** Code Review Agent

**Actions:**
- Verified `GetRegime` RPC in `risk.proto` and implementation in `RiskService`.
- Verified `GridCoordinator` exposes `RegimeMonitor` to `RiskService`.
- Added unit test `TestRiskServiceServer_GetRegime` in `grpc_service_test.go` to verify functionality.
- Confirmed `make proto` generates correct code.

## Notes

- This is a P1 issue for agent-native parity.
