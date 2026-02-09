---
status: pending
priority: p2
issue_id: "036"
tags: [performance, i-o]
dependencies: []
---

# Throttle State Persistence

## Problem Statement
`GridCoordinator` saves state on every price update. For high-frequency symbols, this creates massive I/O overhead.

## Findings
- **Location**: `market_maker/internal/engine/gridengine/coordinator.go:182`
- **Impact**: High latency and CPU/IO usage.

## Proposed Solutions
1. **Option A (Conditional Save)**: Only save if `len(actions) > 0` or if it's the first update after a fill.
2. **Option B (Periodic Save)**: Save at most once every N seconds.

## Recommended Action
Implement a combination: Save immediately if actions are taken, otherwise save periodically (e.g. every 30s).

## Acceptance Criteria
- [ ] State is persisted after order placements/cancellations.
- [ ] State is persisted at least every 60s regardless of activity.

## Work Log
- 2026-02-09: Identified by code-simplicity-reviewer.
