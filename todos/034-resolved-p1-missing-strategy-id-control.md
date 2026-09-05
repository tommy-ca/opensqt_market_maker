---
status: pending
priority: p1
issue_id: "034"
tags: [code-review, agent-native, management]
dependencies: []
---

# Missing Strategy ID Control

'SetStrategyID' is internal-only. Agents cannot manage strategy identity or perform smooth migrations.

## Problem Statement

Agents cannot programmatically configure strategy IDs. 'SetStrategyID' is internal-only, preventing agents from managing strategy identity or performing smooth migrations between different strategy configurations.

## Findings

- `SetStrategyID` exists but is not exposed to external interfaces (gRPC).
- Manual intervention or code changes are required to change strategy IDs.
- Prevents automated agents from managing the lifecycle and identity of strategies.

## Proposed Solutions

### Option 1: Expose via gRPC Management Endpoint

**Approach:** Create or extend a management gRPC service to include `SetStrategyID`.

**Pros:**
- Allows full programmatic control for agents.
- Enables smooth migrations and identity management.

**Cons:**
- Requires careful authorization/security checks as it modifies core identity.

**Effort:** 2-3 hours

**Risk:** Medium (Identity changes can impact tracking and reporting)

## Recommended Action

**To be filled during triage.**

## Technical Details

**Affected files:**
- gRPC proto definitions (Management/Control service)
- Strategy implementation files
- Configuration management logic

## Resources

- Issue ID: 034

## Acceptance Criteria

- [ ] `SetStrategyID` exposed via gRPC.
- [ ] Proper authorization checks implemented for the endpoint.
- [ ] Logic handles identity migration (e.g., updating related metrics/logs) if necessary.

## Work Log

### 2026-02-10 - Initial Creation

**By:** Antigravity

**Actions:**
- Created todo file based on P1 finding.
- Identified need for gRPC management endpoint.

## Notes

- Critical for agent-native management capabilities.
