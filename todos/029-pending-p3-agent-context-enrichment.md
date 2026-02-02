---
status: pending
priority: p3
issue_id: "029"
tags: [ux, code-review]
dependencies: []
---

# Agent Context Enrichment

Suggest adding adjusted_equity and maintenance_margin to SimulateMarginResponse for agents.

## Problem Statement

Currently, `SimulateMarginResponse` provides limited context for agents to make informed decisions during margin simulations. Adding `adjusted_equity` and `maintenance_margin` would provide a more complete picture of the account status after a simulated trade.

## Findings

- `SimulateMarginResponse` currently lacks several key fields that would be useful for automated decision-making.
- Agents need to calculate these values manually or rely on separate calls, which is inefficient and prone to errors.

## Proposed Solutions

### Option 1: Enrich SimulateMarginResponse

**Approach:** Add `adjusted_equity` and `maintenance_margin` fields to the `SimulateMarginResponse` struct.

**Pros:**
- Provides better context for agents.
- Reduces the need for manual calculations or additional API calls.

**Cons:**
- Increases response payload size slightly.

**Effort:** < 1 hour

**Risk:** Low

## Recommended Action

**To be filled during triage.**

## Technical Details

**Affected files:**
- `engine/opensqt_market_maker/models/margin.go` (or wherever `SimulateMarginResponse` is defined)

## Acceptance Criteria

- [ ] `SimulateMarginResponse` includes `adjusted_equity` and `maintenance_margin`.
- [ ] Fields are correctly populated in the margin simulation logic.
- [ ] Existing tests updated and passing.

## Work Log

### 2026-02-01 - Initial Request

**By:** opencode

**Actions:**
- Created todo file for agent context enrichment.

**Learnings:**
- Identified opportunity to improve agent-facing API context.

## Notes
---
