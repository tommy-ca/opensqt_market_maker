---
status: pending
priority: p1
issue_id: "035"
tags: [quality, testing]
dependencies: []
---

# Fix Flaky E2E Regime Tests

## Problem Statement
`TestE2E_RegimeFiltering` in `regime_test.go` uses `time.Sleep` for synchronization. This is slow and unreliable.

## Findings
- **Location**: `market_maker/tests/e2e/regime_test.go`
- **Impact**: Brittle CI/CD pipeline.

## Proposed Solutions
1. **Option A (Eventually)**: Use `assert.Eventually` to poll for the expected order state.

## Recommended Action
Implement Option A.

## Acceptance Criteria
- [ ] `regime_test.go` no longer uses `time.Sleep` for assertions.
- [ ] Tests pass reliably in a loop.

## Work Log
- 2026-02-09: Identified by pattern-recognition-specialist and agent-native-reviewer.
