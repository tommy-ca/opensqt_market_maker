---
date: 2026-02-07
topic: grid-trading-workflow-audit
---

# Grid Trading Workflow Audit & E2E Test Expansion

## What We're Building
A comprehensive review and expansion of the end-to-end (E2E) testing coverage for the market maker's grid trading workflows. This involves auditing the Dual-Engine architecture (Simple vs. Durable), validating critical data flows (Exchange -> Engine -> DB -> Exchange), and implementing rigorous Go-based integration tests to ensure system reliability and parity.

## Why This Approach
We chose **Go Integration Tests** over simulation scripts because the core logic is already written in Go, and we need to verify the *actual* production components (engines, state managers, risk monitors) working together. This ensures our tests catch regressions in the real system, not just a simulation model.

## Key Decisions
- **Scope:** Audit both `SimpleEngine` and `DBOSEngine` workflows.
- **Methodology:**
    1.  **Static Analysis:** Trace data flow for `OrderId` propagation and state recovery.
    2.  **Gap Analysis:** Identify missing recovery scenarios in current tests.
    3.  **Implementation:** Add `TestE2E_DurableRecovery` and `TestE2E_RiskCircuitBreaker` to `market_maker/tests/e2e/`.
- **Target:** Verify that `OrderId` context is preserved during the `InventorySlot` -> `grid.Slot` mapping (a known potential gap).

## Open Questions
- Does the `DBOSEngine` correctly hydrate `SlotManager` state after a hard crash?
- Is the `SyncOrders` reconciliation actually called during the boot sequence?

## Next Steps
→ `/workflows:plan` to execute the audit and write the tests.
