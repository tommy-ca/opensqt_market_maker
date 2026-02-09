---
status: pending
priority: p1
issue_id: "031"
tags: [concurrency, reliability, grid]
dependencies: []
---

# Fix Deadlock in SlotManager

## Problem Statement
There is a circular dependency between the manager-level mutex (`m.mu`) and the individual slot mutexes (`slot.Mu`) in `market_maker/internal/trading/grid/slot_manager.go`.
- `SyncOrders` acquires `m.mu` then calls `trading.ReconcileOrders` which acquires `slot.Mu`.
- `OnOrderUpdate` acquires `slot.Mu` then calls `resetSlotLocked` which attempts to acquire `m.mu`.

## Findings
- **Location**: `market_maker/internal/trading/grid/slot_manager.go`
- **Impact**: Potential freeze of all trading activity during high volatility or boot.

## Proposed Solutions
1. **Option A (Fixed Order)**: Ensure `m.mu` is ALWAYS acquired before `slot.Mu`. In `OnOrderUpdate`, acquire `m.mu` before entering any logic that touches the slot.
2. **Option B (Deferred Map Update)**: Modify `resetSlotLocked` to NOT acquire `m.mu`. Instead, it returns the IDs to be removed from the manager maps, and the caller handles it.

## Recommended Action
Implement Option A: Consistent Lock Ordering (Manager -> Slot).

## Acceptance Criteria
- [ ] Lock acquisition order is consistent across all methods in `SlotManager`.
- [ ] No deadlocks observed in concurrent stress tests.

## Work Log
- 2026-02-09: Identified by architecture-strategist and security-sentinel.
