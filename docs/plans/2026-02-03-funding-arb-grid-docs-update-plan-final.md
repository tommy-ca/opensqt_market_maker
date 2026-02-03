---
title: Review and Update Funding Arb & Grid Trading Models
type: chore
date: 2026-02-03
---

# Review and Update Funding Arb & Grid Trading Models

## Executive Summary (Final - 2026-02-03)

This plan hardens the execution reliability and conceptual integrity of the Funding Arbitrage and Grid Trading systems. It shifts the system from a "command-based" model to a **Declarative Target-State Reconciliation** pattern. This ensures that even in the face of partial fills or process crashes, the system autonomously converges to the desired position state.

## Problem Statement

*   **Logic Duplication**: Fragmented Grid logic increases maintenance risk.
*   **Execution Drift**: Sequential actions without state-based reconciliation lead to delta exposure on partial fills.
*   **Durability Gap**: Transient rebalance loops lack the crash-resilience needed for production multi-pair management.
*   **Documentation Debt**: Monolithic docs lack the clarity needed for both human developers and autonomous agents.

## Proposed Solution

1.  **Converge on "Target-State" Pattern**: Refactor strategies to return an **immutable TargetState** (desired positions/orders). Engines compute the delta between reality and target.
2.  **Consolidate Grid Logic**: Merge all Grid trading logic into `internal/trading/grid/strategy.go`.
3.  **Harden Arb Workflows**: Implement **Atomic Fill-Scaling** using aggressive order types (IOC/FOK) or state-reconciling durable sub-workflows.
4.  **Centralize Risk Bedrock**: Move `MarginSim` / `VME` to `internal/risk/margin` to resolve circular dependencies.
5.  **Majestic Documentation**: Create focused, narrative-driven specs that are clear to humans and structured enough for agents.

## Implementation Phases

### Phase 1: Conceptual Integrity & Grid Consolidation

*   [ ] **Merge**: Consolidate `strategy/grid.go` and `grid/strategy.go` into a single, decoupled `GridStrategy`.
*   [ ] **Refactor**: Update `GridStrategy` to return a `TargetState` struct.
*   [ ] **Document**: Create `grid_strategy_spec.md` focusing on the **Strategy Narrative** (ATR scaling, skew theory) and the machine-readable lifecycle.

### Phase 2: Workflow Hardening (Atomic Neutrality)

*   [ ] **Harden Arb**: Update `ExecuteSpotPerpEntry` to derive the Perp hedge quantity directly from the Spot leg's **immediate execution results**.
*   [ ] **Safety**: Transition to **IOC (Immediate or Cancel)** for entry legs to simplify the "Missing Middle" state machine.
*   [ ] **Atomic Exits**: Update `ExecuteSpotPerpExit` to use `dbos.RunWorkflow` for concurrent closures, reducing directional risk.

### Phase 3: Risk & Margin Bedrock

*   [ ] **Centralize**: Move `MarginSim` / `VME` to `internal/risk/margin`.
*   [ ] **Validation Gates**: Add sharp "Margin Health Gates" with timeouts between rebalance steps in the Portfolio Controller.
*   [ ] **Security**: Audit `factory.go` to ensure `GRPCAPIKey` and `TLSCertFile` are propagated securely and never logged.

### Phase 4: Durable Orchestration

*   [ ] **Controller Upgrade**: Wrap `PortfolioController.Rebalance` in a DBOS workflow to ensure capital allocation "Intents" are reconciled atomically across 20+ pairs.
*   [ ] **Final Audit**: Verify all PR #7 and PR #8 maintenance findings are functional.

## Acceptance Criteria

*   **Zero Duplication**: One definitive implementation of Grid logic.
*   **Atomic Convergence**: Arbitrage system handles partial fills via the reconciler pattern without delta drift.
*   **Safe Emergency**: Failures in one leg of an exit do not block attempts on other legs.
*   **Majestic Docs**: Specifications serve as clear narratives for humans and capability maps for agents.

## References

*   **Review: DHH Rails**: Majestic monolith principles and sharp knives.
*   **Review: Kieran Technical**: Technical excellence and immutable target states.
*   **Review: Simplicity**: Fail-fast reconciler over complex polling.
*   **Arb Spec (026)**: `market_maker/docs/specs/arbitrage_bot_design.md`
