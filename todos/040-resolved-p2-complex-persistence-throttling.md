---
status: pending
priority: p2
issue_id: "040"
tags: [code-review, simplicity]
dependencies: []
---

# Complex Persistence Throttling

'maybeSaveState' uses 'time.AfterFunc' and complex timer logic.

## Problem Statement

The state persistence logic in `maybeSaveState` is overly complex, relying on `time.AfterFunc` and intricate timer management. This makes the code harder to reason about and potentially error-prone.

## Findings

- `maybeSaveState` implementation uses `time.AfterFunc`.
- Complex logic to handle persistence throttling.

## Proposed Solutions

### Option 1: Reactive Cooldown Check

**Approach:** Simplify to a reactive check (dirty flag + cooldown) within the update handlers.

**Pros:**
- Easier to understand and maintain.
- Eliminates manual timer lifecycle management.

**Cons:**
- Logic moves into the hot path (update handlers), though the overhead should be negligible.

**Effort:** 1-2 hours

**Risk:** Low

## Recommended Action

**To be filled during triage.**

## Acceptance Criteria

- [ ] `time.AfterFunc` logic removed.
- [ ] Reactive check (dirty flag + cooldown) implemented in update handlers.
- [ ] State is still persisted correctly according to throttling rules.
- [ ] Tests covering persistence logic pass.

## Work Log

### 2026-02-10 - Initial Creation

**By:** Antigravity

**Actions:**
- Created todo file based on P2 finding.
