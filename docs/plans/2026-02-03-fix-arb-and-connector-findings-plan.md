---
title: Address Code Review Findings for Arbitrage & Connectors
type: fix
date: 2026-02-03
---

# Address Code Review Findings for Arbitrage & Connectors

## Overview

Based on parallel reviews from DHH, Kieran, and Simplicity agents, this plan hardens and simplifies the `arbitrage_bot` and `exchange_connector` entry points. It also continues the "Declarative Target-State" transition into the Arbitrage strategy.

## Problem Statement

*   **Imperative Arbitrage**: Unlike the Grid strategy, the Arbitrage strategy still issues discrete "Actions" (Entry/Exit), which are brittle and prone to state drift.
*   **Startup Boilerplate**: Every `main.go` duplicates config loading, logging setup, and flag-to-env mapping.
*   **Insecure Defaults**: The `exchange_connector` defaults to insecure mode even if credentials exist.
*   **Event Plumbing Mess**: `main.go` manually wires websocket callbacks to engine methods, making it fragile.

## Proposed Solution

1.  **Declarative Arbitrage**: Refactor `ArbitrageStrategy` to return an immutable `TargetState` (Desired position relative to spread). The Engine will reconcile reality to this state.
2.  **Unified Bootstrap**: Create `internal/bootstrap` to handle all startup logic (Config, Logger, Telemetry, Signal handling) in a single call.
3.  **Encapsulated Plumbing**: Move update subscriptions (Price, Order, Funding) inside the `Engine.Start` or a dedicated `EngineRunner`.
4.  **Harden gRPC Server**: Update `ExchangeServer.Serve` to automatically enable TLS and API Key Auth based on the provided configuration.
5.  **Atomic Constants**: Move hardcoded thresholds (Staleness, Risk) to the configuration files.

## Implementation Phases

### Phase 1: Bootstrap & Server Unification (Simplicity & Kieran)

- [ ] **Create `internal/bootstrap`**: Centralize logger, config, and telemetry initialization.
- [ ] **Refactor `ExchangeServer`**: Unify `Start` and `StartWithTLS`. Automatically use `NewExchangeServerWithAuth` if `grpc_api_key` is present.
- [ ] **Validate Config**: Add pre-flight checks for `DatabaseURL` in DBOS mode and `APIKeys` for exchange adapters.

### Phase 2: Arbitrage Bot Consolidation (DHH & Kieran)

- [ ] **Declarative Refactor**: Update `ArbitrageEngine` and `ArbitrageStrategy` to use the `TargetState` pattern.
- [ ] **IOC Enforcement**: Set `TimeInForce_IOC` for all entry orders (durable and simple).
- [ ] **Cleanup `main.go`**: Use the new `bootstrap` package and move subscription wiring into `ArbEngine.Start`.

### Phase 3: Documentation & Audit

- [ ] **Update Spec**: Update `market_maker_design.md` and `arbitrage_bot_design.md` to reflect the declarative shift.
- [ ] **Final Verification**: Run `manage_branches.sh` and `audit_commit_authors.sh`.

## Acceptance Criteria

- [ ] `arbitrage_bot/main.go` and `exchange_connector/main.go` are < 100 lines each.
- [ ] `ArbitrageEngine` implements the same declarative pattern as `GridEngine`.
- [ ] gRPC Auth is enabled automatically when credentials are provided.
- [ ] No unhedged exposure occurs during partial fills (tested via simulation).

## References

- **Review: DHH Rails**: Majestic monolith and declarative shift.
- **Review: Kieran Technical**: IOC enforcement and Auth hardening.
- **Review: Simplicity**: Unified bootstrap and plumbing cleanup.
