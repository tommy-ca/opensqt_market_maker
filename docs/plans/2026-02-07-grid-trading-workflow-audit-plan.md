---
title: Grid Trading Workflow Audit & E2E Test Expansion
type: test
date: 2026-02-07
---

# Grid Trading Workflow Audit & E2E Test Expansion

## Overview

This plan details the steps to audit the grid trading system's robustness, specifically focusing on the Dual-Engine architecture (Simple vs. Durable), crash recovery, and risk management. It culminates in the creation of comprehensive Go-based E2E tests to verify system behavior under failure conditions.

## Problem Statement

Current testing relies heavily on happy-path unit tests. We lack automated verification for:
1.  **Crash Recovery**: Does the system reconcile its state with the exchange after a restart?
2.  **Durable Engine Parity**: Does the DBOS-backed engine behave identically to the Simple engine regarding state persistence?
3.  **Risk Controls**: Does the circuit breaker reliably halt trading across engine restarts?

## Proposed Solution

1.  **Audit**: Static analysis of `durable.go`, `engine.go`, and `slot_manager.go`.
2.  **Gap Analysis**: Verify if `SyncOrders` is called on boot and if `OnOrderUpdate` persists state in the Durable engine.
3.  **Test Implementation**: Create `market_maker/tests/e2e/workflow_test.go` implementing:
    *   `TestE2E_DurableRecovery_OfflineFills`: Simulates a crash and offline exchange activity.
    *   `TestE2E_RiskCircuitBreaker`: Simulates a volatility event and verifies trading halt.

## Implementation Steps

### Phase 1: Test Skeleton Creation
Create `market_maker/tests/e2e/workflow_test.go` with the test structure.

### Phase 2: Implementation of Recovery Test
Implement `TestE2E_DurableRecovery_OfflineFills`:
1.  Initialize Engine with MockExchange and SQLiteStore.
2.  Start Engine -> Price Update -> Orders Placed.
3.  **Simulate Crash**: Stop Engine.
4.  **Simulate Offline Fill**: Manually update MockExchange state (fill an order).
5.  **Simulate Restart**: Start Engine.
6.  **Verify**: Engine state should reflect the filled order (Inventory increased, Order removed).

### Phase 3: Implementation of Risk Test
Implement `TestE2E_RiskCircuitBreaker`:
1.  Initialize Engine with sensitive RiskMonitor.
2.  Simulate Volatility Spike (Price moves 5% in 1 sec).
3.  Verify `IsTriggered` becomes true.
4.  Send subsequent Price Updates.
5.  **Verify**: No new Buy orders are placed despite favorable price.

### Phase 4: Execution & Report
Run the tests. If they fail (as expected based on preliminary analysis), document the failures as findings.

## Acceptance Criteria
- [ ] `market_maker/tests/e2e/workflow_test.go` exists and compiles.
- [ ] Tests cover "Crash -> Offline Fill -> Restart" flow.
- [ ] Tests cover "Risk Trigger -> Halt" flow.
- [ ] Failures are documented (to be fixed in a separate Remediation Plan).

## References
- `docs/brainstorms/2026-02-07-grid-trading-workflow-audit-brainstorm.md`
