---
title: Validate Grid Hardening with Spec-Driven Tests
type: feat
date: 2026-02-11
---

# Validate Grid Hardening with Spec-Driven Tests

## Enhancement Summary

**Deepened on:** 2026-02-11
**Sections enhanced:** 5
**Research agents used:** pattern-recognition-specialist, architecture-strategist, security-sentinel, best-practices-researcher, repo-research-analyst
**Reviewers:** DHH (Rails), Kieran (Quality), Code Simplicity

### Key Improvements
1.  **Architecture Alignment**: Changed target system from `SimpleEngine` to `GridEngine/GridCoordinator`, preventing false positives by testing the actual hardened component.
2.  **Robust Test Patterns**: Incorporated "Kill & Restart" pattern for persistence testing and "Interface-Based Mocking" for Regime Monitor to avoid flaky state management.
3.  **Concurrency Validation**: Added specific checks for "Blocking Mutex I/O" to verify the fix for system freezes during persistence or network lag.
4.  **Observability Validation**: Added explicit verification for Logs and Metrics to ensure visibility into critical regime and risk transitions.
5.  **Flakiness Prevention**: Adopted "Simulated Time" and "Channel Synchronization" patterns to make async tests deterministic.

### New Considerations Discovered
- **System Mismatch**: The existing `setupEngine` helper instantiates `SimpleEngine` which bypasses `GridCoordinator`. A new `setupGridEngine` helper is required.
- **Declarative Verification**: Tests must verify invariant convergence (Equity Balance) rather than just imperative method calls (Mock expectations).
- **Blocking I/O Test**: Simple latency injection is insufficient. Deterministic "Signal-and-Wait" channel patterns are required to prove lock release.

## Overview

This plan establishes a rigoros validation suite for the recent Grid Trading Workflow Hardening (PR #15). We will adopt a **Spec-Driven Development** workflow, first defining the expected behaviors in a structured Markdown specification, and then implementing a comprehensive E2E test suite in Go to verify them.

## Problem Statement / Motivation

Recent critical fixes addressed deadlocks, race conditions, and missing regime logic in the `GridCoordinator` and `RegimeMonitor`. While individual unit tests exist, we lack a cohesive "system behavior" specification that guarantees these components work together correctly under stress. Without this, complex regressions (like the "Ghost Fill" or "Deadlock" issues) could re-emerge unnoticed.

## Proposed Solution

1.  **Define Specification**: Create `docs/specs/034_grid_hardening_validation.md` outlining the exact scenarios for Regime Filtering, Persistence, and Risk Safety.
2.  **Implement Validation Suite**: Create `tests/e2e/grid_hardening_test.go` using `SimulatedExchange` and a new `setupGridEngine` helper to target the correct coordinator logic.

## Technical Approach

### 1. Specification (`docs/specs/034_grid_hardening_validation.md`)

The spec will define the following scenarios using a "Given/When/Then" style (formatted as Markdown tables or lists, not Gherkin):

#### Regime Filtering Scenario
*   **Goal**: Verify Strategy stops buying in Bull Trend and stops selling in Bear Trend.
*   **Test Strategy**: Interface-Based Mocking. Inject `MockRegimeMonitor` to deterministically set `MarketRegime`.
*   **Expectation**:
    *   Set Regime = `BULL_TREND`. Trigger Price Update. Assert `executor` receives **zero** `ORDER_SIDE_SELL` actions.
    *   Set Regime = `HIGH_VOLATILITY`. Trigger Price Update. Assert `executor` receives **zero** actions (if circuit breaker logic applies).
    *   **Time of Day**: Inject `MockClock`. Set time to weekend/off-hours. Verify strategy enters `OFF` or `REDUCE_ONLY` mode (depending on config).
    *   **Observability**: Verify log message "Regime Changed: RANGE -> BULL_TREND" and metric `regime_change_count` increment.

#### Coordinator Persistence Scenario
*   **Goal**: Verify `GridCoordinator` recovers exact state after a crash.
*   **Test Strategy**: "Kill & Restart" Pattern.
    1.  Start Engine A, place orders.
    2.  "Crash" Engine A (stop/discard instance) but KEEP the persistent store (SQLite/File) and Mock Exchange alive.
    3.  **Ghost Fill**: Manually trigger a fill on the Mock Exchange for one of the open orders while the engine is "down".
    4.  Start Engine B (connected to same Store and Exchange).
*   **Expectation**:
    *   **State Restoration**: `LoadState` recovers the `InventorySlot` map and `AnchorPrice`.
    *   **Reconciliation**: The Reconciler fetches open orders from the Exchange, detects the ghost fill, and updates the `InventorySlot` to `FILLED`.
    *   **Invariant Verification**: Verify Total Equity consistency rather than byte-for-byte equality.

#### Risk Safety Scenario
*   **Goal**: Verify Risk Monitor triggers "Reduce Only" mode.
*   **Test Strategy**: Volatility Spike Simulation.
*   **Expectation**:
    *   Inject ATR spike via `MockRiskMonitor`.
    *   Verify `GridCoordinator` immediately cancels all open BUY orders.
    *   Verify subsequent Price Updates do **not** trigger new BUY orders while Risk is triggered.
    *   **Observability**: Verify log "Risk Monitor Triggered" and metric `risk_trigger_count`.

#### Concurrency & Blocking I/O Scenario
*   **Goal**: Verify Coordinator does not freeze during slow I/O.
*   **Test Strategy**: Deterministic Signal-and-Wait.
    1.  **Instrument Exchange**: Create `BlockingExchange` with `ready` and `finish` channels.
    2.  **Trigger Slow Op**: Start `Rebalance` in goroutine. Wait for `ready` signal (confirming it's inside the I/O call).
    3.  **Trigger Fast Op**: Call `OnPriceUpdate` in main thread.
    4.  **Assert**: `OnPriceUpdate` returns immediately (<5ms), proving the lock was released before the slow Exchange call.
    5.  **Cleanup**: Signal `finish` to let `Rebalance` complete.

### 2. Implementation (`tests/e2e/grid_hardening_test.go`)

We will use the existing `market_maker/tests/e2e` package infrastructure but create a specific helper for `GridEngine`.

#### Helper: `setupGridEngine`

```go
// Helper to setup the correct GridEngine (not SimpleEngine)
func setupGridEngine(t *testing.T) (*gridengine.GridEngine, *backtest.SimulatedExchange, *MockRegimeMonitor) {
    // 1. Create SimulatedExchange (Stateful)
    exch := backtest.NewSimulatedExchange()

    // 2. Create MockRegimeMonitor (Stateful Fake)
    regimeMock := &MockRegimeMonitor{Regime: pb.MarketRegime_MARKET_REGIME_RANGE}

    // 3. Initialize GridCoordinator deps
    // ...

    return eng, exch, regimeMock
}
```

#### Test Pattern: Time & Sync

To avoid flakiness:
*   **No `time.Sleep`**: Use channels for coordination as described in the Concurrency Scenario.
*   **Polling**: Use `assert.Eventually` for checking async side effects (like orders appearing in the exchange).
*   **Parallelism**: Use `t.Parallel()` where possible, but ensure each test gets its own isolated Engine/Exchange instance.

## Implementation Phases

### Phase 1: Specification
- [ ] Create `docs/specs/034_grid_hardening_validation.md`.
- [ ] Define all scenarios for Regime, Persistence, Risk, and Boot.

### Phase 2: Test Infrastructure
- [ ] Create `market_maker/tests/e2e/grid_hardening_test.go`.
- [ ] Implement `setupGridEngine` helper to instantiate `gridengine.GridEngine` with `SimulatedExchange`.
- [ ] Create `MockRegimeMonitor` and `MockRiskMonitor` with settable state (Stateful Fakes).
- [ ] Create `BlockingExchange` for concurrency testing.

### Phase 3: Scenario Implementation
- [ ] Implement **Regime Transition** test.
- [ ] Implement **Persistence & Recovery** test (Kill & Restart).
- [ ] Implement **Risk Monitor Safety** test.
- [ ] Implement **Non-Blocking I/O** test (Signal-and-Wait).
- [ ] Implement **Observability Verification** (Logs/Metrics) within the above tests.

## Acceptance Criteria

- [ ] **Spec Created**: `docs/specs/034_grid_hardening_validation.md` exists and covers all 4 functional areas.
- [ ] **Tests Passing**: New `TestGridHardening_*` suite passes consistently (no flakes).
- [ ] **Correct SUT**: Tests explicitly verify `GridCoordinator` logic, not `SimpleEngine`.
- [ ] **State Convergence**: Recovery tests verify financial invariants (Equity) rather than just API calls.
- [ ] **Concurrency Proven**: Tests deterministically prove lock release during I/O.

## Dependencies & Risks

- **Risk**: Test flakiness due to async `RegimeMonitor` updates.
- **Mitigation**: Use `assert.Eventually` with generous timeouts and deterministic mock time if possible.

## References

- Previous Race Fix Spec: `docs/specs/023_reconciliation_race_fix.md`
- Learning: `docs/solutions/test-failures/mock-state-persistence-failure-GridEngine.md`
- Learning: `docs/solutions/performance-issues/blocking-mutex-io-freeze.md`
