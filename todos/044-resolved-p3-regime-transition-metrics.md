---
status: pending
priority: p3
issue_id: "044"
tags: [code-review, observability]
dependencies: []
---

# Regime Transition Metrics

Missing Prometheus/OTel counters for regime changes in the strategy.

## Problem Statement

The system currently lacks observability into when and why strategy regimes change. This makes it difficult to monitor strategy behavior in production and debug unexpected performance issues related to regime transitions.

## Findings

- Missing Prometheus/OTel counters for regime changes.

## Proposed Solutions

### Option 1: Add Prometheus/OTel Counters

**Approach:** Instrument the regime manager to increment a counter every time a transition occurs, including labels for 'from' and 'to' regimes.

**Pros:**
- Improved observability.
- Better visibility into strategy behavior.
- Enables alerting on frequent or unexpected transitions.

**Cons:**
- Slight overhead for metric recording.

**Effort:** 1-2 hours

**Risk:** Low

## Recommended Action

**To be filled during triage.**

## Acceptance Criteria

- [ ] Prometheus/OTel counters added for regime changes.
- [ ] Labels included for 'from' and 'to' regimes.
- [ ] Metrics are verified in the exporter output.

## Work Log

### 2026-02-10 - Task Created

**By:** Antigravity

**Actions:**
- Created todo from P3 finding.
