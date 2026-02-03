---
title: Fix PR #10 Review Findings
type: fix
date: 2026-02-03
---

# Fix PR #10 Review Findings

## Overview

Address critical blocking issues identified in the code review of PR #10.

## Problem Statement

1.  **Security Gap**: `type Secret string` was not implemented, leaving a risk of credential leakage.
2.  **Missing Features**: `GetStatus` gRPC endpoint is missing implementation and Proto definitions.
3.  **Bootstrap Bug**: `App.Run` swallows errors when context is canceled.
4.  **Imperative Leak**: `ArbitrageEngine` still uses imperative `executeEntry` calls instead of pure reconciliation.
5.  **Unused Code**: `GridLevel` struct has fields ignored by the strategy.

## Proposed Solution

1.  **Implement `Secret` Type**: Define `type Secret string` in `internal/config` with a custom `String()` method that returns `[REDACTED]`.
2.  **Implement Observability**: Add `GetStatus` to `ArbitrageEngine` and define `TargetState` messages in `state.proto`.
3.  **Fix Bootstrap**: Update `App.Run` to return the first error even if context is canceled.
4.  **Refactor Arbitrage**: Move `executeEntry/Exit` logic into a `reconcileOrders` method that maps `Delta -> Orders`.
5.  **Cleanup**: Remove unused fields from `GridLevel`.

## Implementation Steps

### Phase 1: Security & Types
- [ ] Create `internal/config/secret.go` with `type Secret string`.
- [ ] Update `Config` struct to use `Secret` for all sensitive fields.
- [ ] Remove manual masking in `Config.String()`.

### Phase 2: Observability & Proto
- [ ] Add `TargetState` message to `market_maker/internal/pb/state.proto`.
- [ ] Implement `GetStatus` in `ArbitrageEngine` to return the current target/actual state.

### Phase 3: Bootstrap Fix
- [ ] Fix `App.Run` error handling logic.
- [ ] Remove empty `Shutdown` method.

### Phase 4: Strategy Cleanup
- [ ] Remove `SlotStatus`, `OrderSide`, `OrderPrice` from `GridLevel`.
- [ ] Update `GridEngine` to populate only required fields.

## Acceptance Criteria
- [ ] Credentials cannot be leaked via `fmt.Printf("%v", config)`.
- [ ] `GetStatus` returns valid data via gRPC.
- [ ] `App.Run` correctly reports startup errors.
- [ ] `GridLevel` contains only fields used by `CalculateTargetState`.
