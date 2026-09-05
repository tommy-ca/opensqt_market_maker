---
status: pending
priority: p2
issue_id: "042"
tags: [code-review, agent-native]
dependencies: []
---

# Missing Indicator Injection

'UpdateFromIndicators' is not exposed to gRPC.

## Problem Statement

The `UpdateFromIndicators` functionality is currently local and not exposed via gRPC. This prevents external agents or systems from feeding indicators or sentiment data into the bot, limiting its "agent-native" capabilities.

## Findings

- `UpdateFromIndicators` method exists but is not reachable via the gRPC API.

## Proposed Solutions

### Option 1: Add gRPC Endpoint

**Approach:** Add a gRPC endpoint that allows external agents to push indicator and sentiment data into the bot.

**Pros:**
- Makes the bot more extensible and agent-friendly.
- Allows for decoupled indicator calculation services.

**Cons:**
- Requires updating the proto definitions and implementing the handler.

**Effort:** 2-3 hours

**Risk:** Low

## Recommended Action

**To be filled during triage.**

## Acceptance Criteria

- [ ] gRPC Service definition updated with `UpdateIndicators` (or similar).
- [ ] gRPC Server implementation calls `UpdateFromIndicators`.
- [ ] Integration test verifying external indicator injection.

## Work Log

### 2026-02-10 - Initial Creation

**By:** Antigravity

**Actions:**
- Created todo file based on P2 finding.
