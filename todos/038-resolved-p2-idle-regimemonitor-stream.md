---
status: pending
priority: p2
issue_id: "038"
tags: [code-review, simplicity]
dependencies: []
---

# Idle RegimeMonitor Stream

Remove the idle 1m Kline stream in `RegimeMonitor` until detection logic is actually implemented.

## Problem Statement

`RegimeMonitor` starts a 1m Kline stream, but the handler is currently a no-op. This is unnecessary overhead (YAGNI) and consumes resources for no benefit.

## Findings

- Kline stream is established and maintained.
- Handler function is empty or does nothing with the data.

## Proposed Solutions

### Option 1: Remove Stream

**Approach:** Delete the code that initiates and manages the 1m Kline stream in `RegimeMonitor`.

**Pros:**
- Reduces complexity.
- Saves network and CPU resources.

**Cons:**
- Will need to be re-implemented when regime detection logic is added.

**Effort:** 0.5 hours

**Risk:** Very Low

## Recommended Action

**To be filled during triage.**

## Technical Details

**Affected files:**
- `internal/monitor/regime.go` (Assumed path)

## Acceptance Criteria

- [ ] Idle Kline stream is removed.
- [ ] `RegimeMonitor` still functions for its other (active) purposes.

## Work Log

### 2026-02-10 - Initial Creation

**By:** Antigravity

**Actions:**
- Created todo from P2 finding 038.
