---
status: pending
priority: p3
issue_id: "031"
tags: [simplification, code-review]
dependencies: []
---

# Simplification Opportunities

Suggest moving to sync.Map in PortfolioController and flattening the rebalance priority logic.

## Problem Statement

The `PortfolioController` currently uses complex synchronization mechanisms that could be simplified using `sync.Map`. Additionally, the rebalance priority logic is nested and difficult to follow. Flattening this logic would improve readability and maintainability.

## Findings

- `PortfolioController` uses manual locking for several maps, which can be replaced with `sync.Map` for better performance and simplicity in some cases.
- The rebalance priority logic has multiple levels of nesting, making it hard to reason about.

## Proposed Solutions

### Option 1: Use sync.Map and Flatten Logic

**Approach:**
- Replace manual locking with `sync.Map` in `PortfolioController` where appropriate.
- Refactor the rebalance priority logic to use a flatter structure (e.g., early returns or a switch statement).

**Pros:**
- Improved code readability and maintainability.
- Potential performance benefits from `sync.Map`.

**Cons:**
- Requires careful refactoring to ensure thread safety is maintained.

**Effort:** 2-4 hours

**Risk:** Medium (due to refactoring core synchronization)

## Recommended Action

**To be filled during triage.**

## Technical Details

**Affected files:**
- `engine/opensqt_market_maker/controllers/portfolio.go`

## Acceptance Criteria

- [ ] `PortfolioController` uses `sync.Map` for relevant maps.
- [ ] Rebalance priority logic is flattened and easier to read.
- [ ] No regression in functionality or thread safety.
- [ ] All tests pass.

## Work Log

### 2026-02-01 - Initial Request

**By:** opencode

**Actions:**
- Created todo file for simplification opportunities.

**Learnings:**
- Identified areas for code cleanup and simplification in `PortfolioController`.

## Notes
---
