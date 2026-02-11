---
status: pending
priority: p3
issue_id: "045"
tags: [code-review, performance]
dependencies: []
---

# String-based Slot Lookups

Using Price.String() as map keys in SlotManager is inefficient.

## Problem Statement

`SlotManager` currently uses the string representation of `Price` as keys in maps. String formatting and comparison are significantly slower than integer operations, leading to unnecessary performance overhead in the hot path of price processing.

## Findings

- `SlotManager` uses `Price.String()` as map keys.

## Proposed Solutions

### Option 1: Use scaled int64 keys

**Approach:** Convert prices to scaled `int64` integers (e.g., multiplying by a fixed factor like 1e8) and use these as map keys.

**Pros:**
- Significant performance improvement for map lookups.
- Reduced memory allocations (no string formatting).
- Faster comparisons.

**Cons:**
- Requires careful handling of precision and scaling factors.

**Effort:** 2-3 hours

**Risk:** Low

## Recommended Action

**To be filled during triage.**

## Acceptance Criteria

- [ ] Map keys in `SlotManager` changed from `string` to `int64`.
- [ ] Benchmarks show improved lookup performance.
- [ ] No loss of precision in price handling.

## Work Log

### 2026-02-10 - Task Created

**By:** Antigravity

**Actions:**
- Created todo from P3 finding.
