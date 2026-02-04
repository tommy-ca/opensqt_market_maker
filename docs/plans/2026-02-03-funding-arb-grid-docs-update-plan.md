---
title: Review and Update Funding Arb & Grid Trading Models
type: chore
date: 2026-02-03
---

# Review and Update Funding Arb & Grid Trading Models

## Overview

This plan executes a comprehensive review and audit of the Funding Rate Arbitrage and Grid Trading systems. It addresses logic duplication in Grid trading, hardens execution workflows for Arbitrage, and upgrades the documentation to reflect the current high-performance "Dual-Engine" and "Durable Orchestrator" architecture.

## Problem Statement

*   **Logic Duplication**: Grid trading logic is split between `internal/trading/strategy/grid.go` and `internal/trading/grid/strategy.go`.
*   **Execution Risk**: Funding Arbitrage workflows may under-hedge if the initial Spot leg fills slowly (delta-neutrality drift).
*   **Durability Gap**: The `PortfolioController.Rebalance` loop is transient and lacks the durable execution guarantees of the strategy engines.
*   **Outdated Specs**: Existing documentation does not fully capture the "Target-State Reconciliation" pattern or the latest Unified Margin (UM) handling.

## Proposed Solution

1.  **Consolidate Grid Strategy**: Merge duplicated Grid logic into a single, decoupled `grid.Strategy` and move it to the **Durable Engine** model (DBOS).
2.  **Harden Arb Workflows**: Enhance the `ExecuteSpotPerpEntry` workflow with active fill monitoring to ensure the Perp hedge matches the actual Spot execution.
3.  **Upgrade Portfolio Durability**: Refactor the Portfolio rebalance loop into a DBOS Durable Workflow.
4.  **Modular Documentation Overhaul**: Split the monolithic design doc into specialized specs:
    *   `grid_strategy_design.md` (Strategy Theory)
    *   `slot_manager_reference.md` (Technical Mechanics)
    *   `durable_grid_roadmap.md` (Migration Plan)

## Technical Approach

### Architecture Refinement

*   **Unified Strategy Interface**: Ensure both Arb and Grid implement a common `TargetStateReconciler` pattern.
*   **Durable Orchestration**: Transition the Portfolio Controller from a periodic goroutine to a DBOS-managed loop.

### Implementation Phases

#### Phase 1: Grid Logic Consolidation & Specs

*   [ ] **Review**: Audit `strategy/grid.go` and `grid/strategy.go` for feature parity.
*   [ ] **Refactor**: Create a unified `GridStrategy` that is purely mathematical and decoupled from I/O.
*   [ ] **Document**: Create the three modular Grid docs proposed in the brainstorm.

#### Phase 2: Workflow Hardening (Arbitrage)

*   [ ] **Audit**: Trace the `ExecuteSpotPerpEntry` workflow for LIMIT order edge cases.
*   [ ] **Harden**: Add a `WaitUntilFill` or `Re-hedge` loop to the durable workflow to maintain atomic neutrality.
*   [ ] **Test**: Write a simulator test with slow-filling orders to verify no delta drift occurs.

#### Phase 3: Portfolio Controller Durability

*   [ ] **Refactor**: Wrap `PortfolioController.Rebalance` in a DBOS workflow.
*   [ ] **Verify**: Ensure "Intents" (Add/Remove Pair) are reconciled atomically even across process restarts.

#### Phase 4: System Guide Consolidation

*   [ ] **Update**: Integrate the Portfolio Controller and UM handling into the `software_design_document.md`.
*   [ ] **Final Audit**: Verify all cross-references in `docs/specs/` are valid and up-to-date.

## Acceptance Criteria

*   [ ] **Zero Duplication**: Only one implementation of Grid trading logic remains.
*   [ ] **Atomic Neutrality**: Arbitrage engine correctly hedges partial/slow fills without manual intervention.
*   [ ] **Durable Rebalance**: Portfolio rebalance state is persisted and resumed by DBOS after a crash.
*   [ ] **Comprehensive Docs**: Modular Grid specs and updated System Architecture guide are merged.

## References

*   **Brainstorm**: `docs/brainstorms/2026-02-02-grid-docs-refresh-brainstorm.md`
*   **Arbitrage Spec (026)**: `market_maker/docs/specs/arbitrage_bot_design.md`
*   **Orchestrator Spec**: `docs/specs/phase18_orchestrator_tech_spec.md`
*   **Grid Audit**: `docs/specs/grid_workflow_audit_spec.md`
