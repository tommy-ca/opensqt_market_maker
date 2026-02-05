---
title: Fix PR #10 Post-Merge Broken State
type: fix
date: 2026-02-03
---

# Fix PR #10 Post-Merge Broken State

## Overview

The previous attempt to merge PR #10 (and its associated fixes) resulted in a broken codebase on `main`. The `review` command detected:
1.  **Compilation Errors**: Missing methods and types in tests.
2.  **Unused Code**: `internal/bootstrap` and `TargetState` are present but not integrated.
3.  **Security Flaws**: `ExchangeConfig` still uses `string` for secrets despite the `Secret` type existing.
4.  **Incomplete Refactor**: The `ArbitrageEngine` and `GridEngine` were not actually updated to use the new `TargetState` pattern, leading to dead code and broken tests.

## Problem Statement

The merge of `feat/arb-connector-refactor` seems to have missed applying the core changes to the engine files (`arbengine/engine.go`, `gridengine/engine.go`) while applying the supporting changes (`core/types.go`, `config/secret.go`). This has left the repository in an inconsistent state where new types exist but are unused, and tests are failing because they expect the new logic.

## Proposed Solution

A comprehensive fix branch `fix/restore-pr10-logic` to re-apply the lost changes and finish the integration.

1.  **Restore Engine Logic**: Update `ArbitrageEngine` and `GridEngine` to use `TargetState`.
2.  **Fix Configuration**: Update `ExchangeConfig` to use `Secret` type.
3.  **Integrate Bootstrap**: Update `main.go` files to use `internal/bootstrap`.
4.  **Fix Tests**: Update all broken tests to match the new engine signatures.

## Implementation Steps

### Phase 1: Restore Core Logic
- [ ] Update `market_maker/internal/engine/arbengine/engine.go`: Implement `GetStatus` and `reconcile` (declarative loop).
- [ ] Update `market_maker/internal/engine/gridengine/engine.go`: Implement `GetStatus` and usage of `CalculateTargetState`.
- [ ] Update `market_maker/internal/trading/grid/strategy.go`: Ensure `CalculateTargetState` matches the interface.

### Phase 2: Security & Config
- [ ] Update `market_maker/internal/config/config.go`: Change sensitive fields to `Secret` type.
- [ ] Update `market_maker/internal/exchange/server.go`: Ensure `StartWithTLS` is correctly integrated or replaced by `Serve`.

### Phase 3: Bootstrap Integration
- [ ] Update `market_maker/cmd/arbitrage_bot/main.go` to use `bootstrap.NewApp`.
- [ ] Update `market_maker/cmd/exchange_connector/main.go` to use `bootstrap.NewApp`.
- [ ] Update `market_maker/cmd/market_maker/main.go` to use `bootstrap.NewApp`.

### Phase 4: Test Fixes
- [ ] Fix `grid_test.go`, `dynamic_grid_test.go`, `trend_following_test.go` imports and types.
- [ ] Verify `go build ./...` passes.

## Acceptance Criteria
- [ ] `go build ./...` succeeds.
- [ ] `go test ./...` succeeds.
- [ ] `GetStatus` is implemented and exposed.
- [ ] Secrets are masked in logs.
