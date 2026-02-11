---
status: pending
priority: p2
issue_id: "035"
tags: [code-review, architecture]
dependencies: []
---

# Non-deterministic Exchange Selection

Use a config-driven selection or sorted keys in `NewGridCoordinator` to avoid non-deterministic behavior.

## Problem Statement

`NewGridCoordinator` picks the first entry in an `Exchanges` map. Since maps in Go are non-deterministic during iteration, this can lead to different exchanges being selected across different runs, which is undesirable for consistency.

## Findings

- `NewGridCoordinator` relies on map iteration order which is randomized in Go.

## Proposed Solutions

### Option 1: Config-driven Selection

**Approach:** Explicitly specify the primary exchange in the configuration.

**Pros:**
- Most explicit and controllable.
- Easy to change without code modification.

**Cons:**
- Requires configuration update.

**Effort:** 1 hour

**Risk:** Low

---

### Option 2: Sorted Keys

**Approach:** Collect map keys, sort them, and always pick the first one (e.g., alphabetically).

**Pros:**
- Guaranteed determinism without configuration changes.

**Cons:**
- Might pick an exchange that wasn't intended to be the "primary" one if multiple are provided.

**Effort:** 0.5 hours

**Risk:** Low

## Recommended Action

**To be filled during triage.**

## Technical Details

**Affected files:**
- `internal/coordinator/grid.go` (Assumed path based on name)

## Acceptance Criteria

- [ ] Exchange selection is deterministic across restarts.
- [ ] Selection logic is documented or clearly configurable.
- [ ] Unit test verifies deterministic selection.

## Work Log

### 2026-02-10 - Initial Creation

**By:** Antigravity

**Actions:**
- Created todo from P2 finding 035.
