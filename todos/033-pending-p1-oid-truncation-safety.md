---
status: pending
priority: p1
issue_id: "033"
tags: [security, reliability, exchange]
dependencies: []
---

# Secure ClientOrderId from Truncation Collision

## Problem Statement
`AddBrokerPrefix` in `helpers.go` prepends a broker prefix and then truncates the result to 36 characters. If the original OID (which includes unique price/side digits) is pushed past the 36-char limit, multiple orders will have identical IDs.

## Findings
- **Location**: `market_maker/pkg/pbu/helpers.go`
- **Impact**: Order collisions on exchanges (Binance), leading to failed trades or state corruption.

## Proposed Solutions
1. **Option A (Left Truncation)**: Truncate the *prefix* part instead of the *suffix* part.
2. **Option B (Smart Truncation)**: Truncate the StrategyID component of the OID while preserving the unique price/side code at the end.

## Recommended Action
Implement Option B: Preserve the unique suffix (the last ~10-15 chars) and truncate the middle/prefix if needed.

## Acceptance Criteria
- [ ] `GenerateDeterministicOrderID` and `AddBrokerPrefix` combined never produce duplicate IDs for different grid levels/sides.
- [ ] Added unit tests for long StrategyIDs and prefixes.

## Work Log
- 2026-02-09: Identified by security-sentinel.
