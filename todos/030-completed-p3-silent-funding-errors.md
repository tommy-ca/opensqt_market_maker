---
status: completed
priority: p3
issue_id: "030"
tags: [reliability, code-review]
dependencies: []
---

# Silent Funding Errors

Suggest adding logging/alerting for failed funding rate fetches instead of silent return.

## Problem Statement

Currently, when a funding rate fetch fails, the system returns silently. This can lead to stale or incorrect data being used in calculations without any indication of a problem. Adding logging or alerting would help identify and resolve these failures more quickly.

## Findings

- Funding rate fetches are critical for accurate portfolio management and rebalancing.
- Silent failures make it difficult to monitor the health of exchange data integrations.

## Proposed Solutions

### Option 1: Add Logging and Alerting

**Approach:** Implement logging and optionally alerting when a funding rate fetch fails.

**Pros:**
- Improved visibility into system health.
- Faster identification of data integration issues.

**Cons:**
- Potential for log noise if fetch failures are frequent and expected (though they shouldn't be).

**Effort:** 1-2 hours

**Risk:** Low

## Recommended Action

**To be filled during triage.**

## Technical Details

**Affected files:**
- `engine/opensqt_market_maker/exchange/binance.go` (or wherever funding rate fetching is implemented)

## Acceptance Criteria

- [x] Fetch failures are logged with relevant details (e.g., symbol, error message).
- [ ] Alerting is triggered for persistent failures (if applicable).
- [ ] Tests verify that errors are handled and logged correctly.

## Work Log

### 2026-02-01 - Initial Request

**By:** opencode

**Actions:**
- Created todo file for silent funding errors.

**Learnings:**
- Identified reliability risk due to silent failures.

## Notes
---
