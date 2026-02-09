---
status: pending
priority: p2
issue_id: "037"
tags: [reliability, grid]
dependencies: []
---

# Harden Reconciler against Ghost Fills

## Problem Statement
In `ReconcileOrders`, if a slot is `LOCKED` but the order is missing from the exchange, it is cleared. This ignores the possibility that the order was FILLED.

## Findings
- **Location**: `market_maker/internal/trading/reconciler.go`
- **Impact**: Duplicate orders and inventory drift.

## Proposed Solutions
1. **Option A (Position Check)**: Compare total exchange position against local filled slots during reconciliation. If exchange > local, some missing orders were fills.

## Recommended Action
Implement basic drift detection in `RestoreFromExchangePosition` to identify these ghost fills.

## Acceptance Criteria
- [ ] System logs a critical warning or halts if position drift is detected during boot.

## Work Log
- 2026-02-09: Identified by architecture-strategist and security-sentinel.
