---
title: Grid Trading Workflow Audit & E2E Test Expansion (Deepened)
type: test
date: 2026-02-08
---

# Grid Trading Workflow Audit & E2E Test Expansion

## Enhancement Summary

**Deepened on:** 2026-02-08
**Sections enhanced:** 4
**Research agents used:** architecture-strategist, security-sentinel, performance-oracle, code-simplicity-reviewer, agent-native-reviewer, best-practices-researcher, framework-docs-researcher

### Key Improvements
1.  **Unified Orchestration Pattern**: Refactored implementation strategy to use a shared `GridOrchestrator` base class to guarantee logic parity between Simple and Durable engines by design.
2.  **Mandatory Boot Reconciliation**: Added explicit "Exchange Sync" phase to the engine startup sequence to eliminate the "Zombie Order" risk (restoring stale local state).
3.  **Performance-First State Management**: Optimized `SlotManager` to use integer-based tick keys instead of string-based decimal keys, reducing lock contention during high-frequency updates.
4.  **Resilience Fault-Injection**: Enhanced E2E test plan to include "Chaos Proxy" scenarios (injecting 429 Rate Limits and 5xx Errors) to verify robust error handling.

---

## Overview

This plan details the steps to audit the grid trading system's robustness, specifically focusing on the Dual-Engine architecture (Simple vs. Durable), crash recovery, and risk management. It culminates in the creation of comprehensive Go-based E2E tests to verify system behavior under failure conditions.

### Research Insights

**Architecture Strategist:**
- The current duplication of orchestration logic between `GridEngine` and `DBOSGridEngine` is a high-risk violation of DRY. Logic will inevitably diverge.
- **Recommendation**: Refactor to a shared orchestrator that handles strategy logic and state transitions, delegating execution to a `GridExecutor` interface.

**Performance Oracle:**
- Sequential startup sync for multiple pairs will be a major bottleneck.
- **Recommendation**: Parallelize `FetchOpenOrders` calls across all symbols using `errgroup` during bootstrap.

---

## Problem Statement

Current testing relies heavily on happy-path unit tests. We lack automated verification for:
1.  **Crash Recovery**: Does the system reconcile its state with the exchange after a restart?
2.  **Durable Engine Parity**: Does the DBOS-backed engine behave identically to the Simple engine regarding state persistence?
3.  **Risk Controls**: Does the circuit breaker reliably halt trading across engine restarts?

### Research Insights

**Security Sentinel:**
- **Idempotency is Key**: Every `PlaceOrder` request MUST use a deterministic `client_order_id` (hash of StrategyID + Level + TimestampRound) to prevent double-fills during network retries.
- **Data Integrity**: Ensure `OnOrderUpdate` (fills) triggers an atomic persistence event to avoid state loss if a crash occurs before the next price tick.

---

## Proposed Solution

1.  **Audit**: Static analysis of `durable.go`, `engine.go`, and `slot_manager.go`.
2.  **Gap Analysis**: Verify if `SyncOrders` is called on boot and if `OnOrderUpdate` persists state in the Durable engine.
3.  **Test Implementation**: Create `market_maker/tests/e2e/workflow_test.go` implementing:
    *   `TestE2E_DurableRecovery_OfflineFills`: Simulates a crash and offline exchange activity.
    *   `TestE2E_RiskCircuitBreaker`: Simulates a volatility event and verifies trading halt.

### Research Insights

**Best Practices:**
- **The "Ghost Fill" Scenario**: Specifically test the bot's ability to detect an order that was filled while the bot was down. The `Reconciler` must automatically transition the grid state upon discovery.
- **Differential Fuzzing**: Run both engines in "Shadow Mode" against the same recorded market data and assert that their produced `OrderActions` are identical.

**Go Testing Patterns:**
- Use `assert.Eventually` instead of `time.Sleep` for more responsive and reliable E2E tests.
- Utilize `t.TempDir()` for SQLite database isolation to ensure clean states for every test run.

---

## Implementation Steps

### Phase 1: Test Skeleton & Mock Hardening
Create `market_maker/tests/e2e/workflow_test.go`.
- **Mock Hardening**: Update `MockPositionManager` to ensure `ApplyActionResults` actually persists state in-memory (fixing the silent drop bug documented in `docs/solutions/test-failures/`).

### Phase 2: Implementation of Recovery Test
Implement `TestE2E_DurableRecovery_OfflineFills`:
1.  Initialize Engine with `MockExchange` and `SQLiteStore`.
2.  Start Engine -> Send Price -> Wait for `PLACE` (use `Eventually`).
3.  **Simulate Crash**: Stop Engine.
4.  **Offline Action**: Manually fill an order in `MockExchange`.
5.  **Simulate Restart**: Start Engine.
6.  **Verify**: Engine must call `SyncOrders` on boot and reflect the filled status (Inventory updated, Buy-back order placed).

### Phase 3: Chaos & Fault Injection
Expand tests to include:
1.  **Rate Limit Test**: Configure `FaultyExchange` to return `429` on every 3rd request. Verify bot backs off without losing state.
2.  **State Mismatch Test**: Manually corrupt the SQLite state checksum and verify the bot refuses to start (Data Integrity Guardian).

### Phase 4: Comparative Parity Suite
Refactor E2E tests into a shared suite:
```go
func TestEngines(t *testing.T) {
    engines := []EngineFactory{NewSimpleEngine, NewDBOSEngine}
    for _, factory := range engines {
        t.Run(factory.Name(), func(t *testing.T) {
            runRecoveryScenario(t, factory)
            runRiskScenario(t, factory)
        })
    }
}
```

---

## Acceptance Criteria
- [ ] `market_maker/tests/e2e/workflow_test.go` exists and compiles.
- [ ] Tests cover "Crash -> Offline Fill -> Restart" flow with **explicit reconciliation check**.
- [ ] Tests cover "Risk Trigger -> Halt" flow across **both engines**.
- [ ] `SyncOrders` is verified to be called during `Start()` sequence.
- [ ] `client_order_id` generation is verified to be deterministic.

## References
- `docs/brainstorms/2026-02-07-grid-trading-workflow-audit-brainstorm.md`
- `docs/solutions/test-failures/mock-state-persistence-failure-GridEngine.md`
- `docs/solutions/architecture-patterns/declarative-reconciliation.md`
