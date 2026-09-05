---
status: pending
priority: p2
issue_id: "036"
tags: [code-review, quality]
dependencies: []
---

# Missing Shutdown Logic

Implement `Stop()` in `GridCoordinator` and call it from `GridEngine` to prevent goroutine leaks.

## Problem Statement

`Stop()` in `GridEngine` is currently a no-op, but `Start()` initiates monitors and streams. This means there's no way to gracefully shut down these processes, leading to goroutine leaks when the engine is supposed to stop.

## Findings

- `GridEngine.Stop()` is empty.
- `GridEngine.Start()` spawns background tasks (monitors, streams).

## Proposed Solutions

### Option 1: Full Shutdown Implementation

**Approach:** Implement `Stop()` method in `GridCoordinator` and all internal components (monitors, stream managers). Use context cancellation or stop channels to signal exit.

**Pros:**
- Clean resource management.
- Prevents memory and goroutine leaks.

**Cons:**
- Requires touching multiple components to ensure propagation of the stop signal.

**Effort:** 2-3 hours

**Risk:** Low

## Recommended Action

**To be filled during triage.**

## Technical Details

**Affected files:**
- `internal/engine/grid.go`
- `internal/coordinator/grid.go`

## Acceptance Criteria

- [ ] `GridEngine.Stop()` successfully stops all background goroutines.
- [ ] No goroutine leaks detected in tests after calling `Stop()`.
- [ ] `Stop()` handles multiple calls gracefully (idempotency).

## Work Log

### 2026-02-10 - Initial Creation

**By:** Antigravity

**Actions:**
- Created todo from P2 finding 036.
