---
status: resolved
priority: p1
issue_id: "031"
tags: [concurrency, data-integrity, trading]
dependencies: []
---

# PR #15 Critical Fixes: Concurrency & Data Integrity

## Problem Statement
The Grid Trading Workflow Hardening (PR #15) has introduced critical concurrency and data integrity bugs that risk system panics and financial loss (via inventory corruption).

## Findings
1.  **✅ Deadlock in SuperPositionManager**: Fixed by establishing consistent Manager -> Slot hierarchy.
2.  **✅ Concurrent Map Access**: Fixed by holding RLock during strategy slot conversion and snapshotting.
3.  **✅ Ghost Fill Corruption**: Fixed. Using `OriginalQty` instead of `OrderPrice` in reconciler.
4.  **✅ Double-Execution Race**: Fixed by calling `MarkSlotsPending` while locked in `OnPriceUpdate`.
5.  **✅ Dead RegimeMonitor**: Fixed by calling `rm.Start(ctx)` and `monitor.Start(ctx)` in `GridCoordinator.Start()`.

## Proposed Solutions
1.  **Fix Deadlock**: Establish a consistent `Manager -> Slot` lock hierarchy. Refactor `resetSlotLocked` to operate under the manager lock.
2.  **Fix Map Race**: Iterate over a snapshot/copy of the slots map or hold the `RLock` for the duration of the calculation.
3.  **Fix Inventory**: Use `OriginalQty` (stored in slot) instead of `OrderPrice` in `reconciler.go`.
4.  **Fix Double-Execution**: Call `MarkSlotsPending` while locked in `OnPriceUpdate` before releasing for execution.
5.  **Fix Lifecycle**: Call `rm.Start(ctx)` in `GridCoordinator.Start()`.

## Recommended Action
Implement all P1 fixes immediately.

## Acceptance Criteria
  - [x] `go test -race ./...` passes.
  - [x] E2E recovery test asserts correct `PositionQty`.
  - [x] `RegimeMonitor` log confirmation of Kline subscription.

## Work Log
- 2026-02-09: Findings consolidated from multiple agents.
- 2026-02-10: All P1 fixes implemented and verified with race detector and E2E tests.
