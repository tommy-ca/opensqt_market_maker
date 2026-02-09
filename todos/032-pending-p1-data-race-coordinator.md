---
status: pending
priority: p1
issue_id: "032"
tags: [concurrency, quality, grid]
dependencies: []
---

# Fix Data Race in GridCoordinator

## Problem Statement
`GridCoordinator.OnPriceUpdate` iterates over slots and accesses fields like `Price`, `PositionStatus`, and `PositionQty` without acquiring the per-slot mutex (`s.Mu`). These fields are modified concurrently by other streams.

## Findings
- **Location**: `market_maker/internal/engine/gridengine/coordinator.go:161-173`
- **Impact**: Inconsistent state during strategy calculation, potentially leading to incorrect orders.

## Proposed Solutions
1. **Option A (Manual Locking)**: Add `s.Mu.RLock()` / `s.Mu.RUnlock()` around the field access inside the loop.
2. **Option B (Snapshotting)**: Use a `GetSnapshot()`-like method that returns a deep copy of the slots with safe locking.

## Recommended Action
Implement Option A for minimal overhead in the hot path.

## Acceptance Criteria
- [ ] Slot field access in `OnPriceUpdate` is protected by `s.Mu.RLock()`.
- [ ] `go test -race` passes for grid engine tests.

## Work Log
- 2026-02-09: Identified by performance-oracle.
