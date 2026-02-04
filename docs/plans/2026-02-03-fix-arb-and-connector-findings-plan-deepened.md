---
title: Address Code Review Findings for Arbitrage & Connectors
type: fix
date: 2026-02-03
---

# Address Code Review Findings for Arbitrage & Connectors

## Enhancement Summary

**Deepened on:** 2026-02-03
**Sections enhanced:** 5
**Research agents used:** DHH Reviewer, Kieran Technical Reviewer, Security Sentinel, Performance Oracle, Code Simplicity Reviewer, Go Bootstrap Researcher, gRPC Security Expert, HFT Strategy Researcher

### Key Improvements
1.  **Security Hardening**: Replaced brittle redaction with a custom **`Secret` type** that automatically masks itself in logs. The system now enforces TLS whenever gRPC API keys are used.
2.  **Lean Omakase Bootstrap**: Created a high-utility `internal/bootstrap` package that returns a graceful `context.Context` and configured `slog`. Implementation logic is kept out of the bootstrap to avoid "Junk Drawer" bloat.
3.  **Primacy of State**: Refactored Arbitrage into a pure **Target-State Reconciliation** model. This eliminates the "Missing Middle" problem by computing $\Delta = Target - (Realized + InFlight)$ on every tick.
4.  **Simplified Rebalancer**: Removed redundant state machines and manual triggers. The system relies purely on the delta equation for autonomous self-healing.
5.  **Agent-Native Observability**: Exposed the raw `TargetState` and `ActualPosition` via gRPC to allow autonomous agents to audit convergence without micromanaging.

## Overview

Based on parallel reviews from DHH, Kieran, and Simplicity agents, this plan hardens and simplifies the `arbitrage_bot` and `exchange_connector` entry points. It also continues the "Declarative Target-State" transition into the Arbitrage strategy, favoring "Primacy of State" over complex imperative transitions.

## Problem Statement

*   **Imperative Arbitrage**: Strategies issue discrete commands, which are brittle and fail to reconcile after network timeouts or partial fills.
*   **Startup Boilerplate**: `main.go` files are cluttered with redundant logging, telemetry, and flag logic.
*   **Insecure Defaults**: Credentials can be leaked to logs and sent over plaintext gRPC.
*   **Competing Patterns**: The presence of both a state machine and a delta reconciler creates logical weight and "out-of-sync" risks.

## Proposed Solution

1.  **Declarative Arbitrage**: Refactor `ArbitrageStrategy` to be a pure function returning an immutable `TargetState`.
2.  **Lean Bootstrap**: Centralize environment setup into `internal/bootstrap` using a simple `Init()` function that handles signals and logging.
3.  **Secret Safety**: Introduce a `type Secret string` that redacts itself in logs via `String()` and `GormValue()` overrides.
4.  **Delta-Only Reconciliation**: Collapse the complex state machine into a single Delta Equation: `Action = Target - (Realized + InFlight)`.
5.  **Harden gRPC Server**: Unify server entry points and enforce "Secure by Default" transport for all authenticated requests.

## Implementation Phases

### Phase 1: Bootstrap & Secret Safety (Simplicity & Kieran)

- [ ] **Create `internal/bootstrap`**: Centralize logger (`slog`) and signal handling.
- [ ] **Implement `type Secret string`**: Apply to `APIKey`, `SecretKey`, and `Passphrase` to prevent log leakage.
- [ ] **Refactor `ExchangeServer`**: Implement a unified `Serve(cfg)` method that enforces TLS when Auth is present.
- [ ] **Validate Config**: Add pre-flight checks for `DatabaseURL` and filesystem permissions on TLS `.key` files.

### Phase 2: Arbitrage Bot Consolidation (DHH & Kieran)

- [ ] **Declarative Refactor**: Transition `ArbitrageEngine` to the "Primacy of State" model.
- [ ] **Pure Strategy**: Move logic to `internal/trading/arbitrage/strategy.go` as `func(MarketState) TargetState`.
- [ ] **Harden Delta Equation**: Ensure `InFlight` quantity correctly accounts for `PENDING_CANCEL` states to prevent double-hedging.
- [ ] **IOC Enforcement**: Set `TimeInForce_IOC` for entry legs. If Leg A fills 0%, the reconciler simply waits (Self-healing).

### Phase 3: Agent-Native Observability

- [ ] **Expose State**: Add gRPC `GetStatus` endpoint returning raw `TargetState` vs `ActualPosition`.
- [ ] **Remove YAGNI**: Deleted proposed `ForceReconcile` and `Status` enums in favor of raw state visibility.

## Research Insights

### Best Practices: Go Bootstrap & Slog (v1.22+)
- **Pattern**: `Init() (ctx, logger)` handles `SIGTERM` internally and provides a house-standard logger.
- **Signal Handling**: Use `signal.NotifyContext` to propagate cancellation from `SIGTERM` down to individual exchange streams.

### Technical Excellence: The Delta Equation
- **Algorithm**: The Reconciler should compute: `Action = TargetPosition - (RealizedPosition + InFlightOrders)`.
- **Robustness**: The "Passive-Aggressive" pattern is handled naturally: fire a Limit order on A; the next tick sees `RealizedPosition` A changed and fires an IOC on B.

### Security Considerations: Credential Guard
- **Masking**: The custom `Secret` type makes it impossible to leak credentials via `%v` or `%+v` in logs.
- **TLS Enforcement**: The server should refuse to enable Auth if TLS is disabled, protecting credentials from interception.

## Acceptance Criteria

- [ ] `arbitrage_bot/main.go` is < 100 lines of code.
- [ ] No secrets appear in logs, even with verbose logging.
- [ ] Reconciler handles a 40% partial fill by scaling the hedge leg to exactly 40% on the next cycle.
- [ ] Autonomous agents can view the `TargetState` via gRPC.


## References

- **Learning: Declarative Reconciliation**: `docs/solutions/architecture-patterns/declarative-reconciliation.md`
- **Review: DHH Rails**: Omakase startup and reduced mental overhead.
- **Review: Kieran Technical**: Robust reconciliation and pure strategy functions.
- **Security Audit**: High-severity fix for `Config.String()` log leakage.
