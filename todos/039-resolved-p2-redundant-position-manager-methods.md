---
status: resolved
priority: p2
issue_id: "039"
tags: [code-review, simplicity]
dependencies: []
---

# Redundant IPositionManager Methods

'CalculateAdjustments' and duplicate 'CreateReconciliationSnapshot' bloat the interface.

## Problem Statement

The `IPositionManager` interface contains redundant orchestration methods that lead to interface bloat and potential confusion. Specifically, `CalculateAdjustments` and a duplicate `CreateReconciliationSnapshot` (if confirmed) are unnecessary overhead.

## Findings

- `CalculateAdjustments` is currently part of the manager interface but acts more as an orchestration step.
- Redundancy in `CreateReconciliationSnapshot` methods.

## Proposed Solutions

### Option 1: Consolidate and Clean Interface

**Approach:** Consolidate `GetSlots` and remove redundant orchestration methods from the manager.

**Pros:**
- Slimmer interface.
- Better separation of concerns.

**Cons:**
- May require refactoring callers of the removed methods.

**Effort:** 1-2 hours

**Risk:** Low

## Recommended Action

**To be filled during triage.**

## Acceptance Criteria

- [ ] Redundant methods removed from `IPositionManager` interface.
- [ ] Implementations updated to remove or internalize these methods.
- [ ] `GetSlots` consolidated as planned.
- [ ] All tests passing.

## Work Log

### 2026-02-10 - Initial Creation

**By:** Antigravity

**Actions:**
- Created todo file based on P2 finding.
