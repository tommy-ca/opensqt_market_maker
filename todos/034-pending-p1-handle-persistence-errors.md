---
status: pending
priority: p1
issue_id: "034"
tags: [reliability, architecture]
dependencies: []
---

# Handle Ignored Persistence Errors

## Problem Statement
`GridCoordinator` ignores error returns from `RestoreState` and `SaveState`. This can lead to the bot running with stale or corrupted state if the DB/store fails.

## Findings
- **Location**: `market_maker/internal/engine/gridengine/coordinator.go`
- **Impact**: Silent failures in state management.

## Proposed Solutions
1. **Option A (Strict Error Handling)**: Return errors from `Start()` if `RestoreState` fails. Panic or halt if `SaveState` fails during critical updates.
2. **Option B (Logged Errors)**: At minimum, log the error and increment an error metric (not recommended for "Hardening").

## Recommended Action
Implement Option A: Return errors from `Start` and ensure `OnPriceUpdate` handles persistence failure appropriately (e.g., skip update if state can't be saved).

## Acceptance Criteria
- [ ] `GridCoordinator.Start` returns error if restoration fails.
- [ ] `GridCoordinator.OnPriceUpdate` returns error if `saveState` fails.

## Work Log
- 2026-02-09: Identified by pattern-recognition-specialist.
