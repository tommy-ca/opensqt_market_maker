---
title: Review and Update Funding Arb & Grid Trading Models
type: chore
date: 2026-02-03
---

# Review and Update Funding Arb & Grid Trading Models

## Enhancement Summary

**Deepened on:** 2026-02-03
**Sections enhanced:** 4
**Research agents used:** Security Sentinel, Architecture Strategist, Performance Oracle, Code Simplicity Reviewer, Agent-Native Reviewer, DBOS Expert

### Key Improvements
1.  **Security Hardening**: Propagate TLS/Auth configuration through `factory.go` and add semantic input validation to all durable workflows.
2.  **Target-State Alignment**: Refactor both Arb and Grid to return **TargetState** (desired positions/orders) rather than discrete execution actions, enabling natural reconciliation of partial fills.
3.  **Atomic Parallel Exits**: Harden `ExecuteSpotPerpExit` to close both legs concurrently using `dbos.RunWorkflow` to minimize directional slippage.
4.  **Agent-Native Documentation**: Added machine-readable context blocks and YAML frontmatter to all new specifications to support autonomous auditing.

### New Considerations Discovered
- **Bypass Risk**: Env var overrides (`GIT_AUTHOR_NAME`) can bypass simple identity checks; updated plan to use `git var` for validation.
- **Race Conditions**: Sequential slot updates in Grid can be slow; parallelizing via independent workflows is recommended.
- **YAGNI**: Keep the Portfolio Controller loop transient; focus durability on the *Intents* and *Orders* it produces.

## Overview

This plan executes a comprehensive review and audit of the Funding Rate Arbitrage and Grid Trading systems. It hardens execution workflows, addresses logic duplication, and establishes a new standard for agent-native technical documentation.

## Problem Statement

*   **Logic Duplication**: Grid logic is fragmented between `strategy/grid.go` and `grid/strategy.go`.
*   **Execution Risk**: Funding Arb may under-hedge on slow Spot fills; Sequential exits increase directional risk.
*   **Durability Gap**: Multi-pair rebalancing is transient and lacks the "Validation Gates" needed for safe margin movement.
*   **Outdated Specs**: Monolithic docs lack machine-readable context for autonomous agents.

## Proposed Solution

1.  **Consolidate Grid Strategy**: Merge logic into a decoupled `GridStrategy` that returns a **TargetState**.
2.  **Harden Arb Workflows**: Implement **Parallel Exits** and **Atomic Fill-Scaling** (using IOC/FOK where possible or durable polling sub-workflows).
3.  **Centralize Margin Logic**: Move `VirtualMarginEngine` / `MarginSim` to a dedicated `internal/risk/margin` package to resolve circular dependencies.
4.  **Modular Agent-Native Documentation**: Split docs into specialized, machine-readable specifications.

## Technical Approach

### Architecture Refinement

*   **Target-State Reconciler**: Engines (Durable State Machines) reconcile the current state to the strategy's target.
*   **Security Propagation**: Ensure `GRPCAPIKey` and `TLSCertFile` are synced from `live_server` config through to the exchange connector.

### Implementation Phases

#### Phase 1: Logic Consolidation & Specs (Agent-Native)

*   [ ] **Consolidate**: Merge duplicated logic into `internal/trading/grid/strategy.go`.
*   [ ] **Specs**: Create `grid_strategy_spec.md` and `slot_manager_reference.md` with:
    *   **YAML Frontmatter**: (tags, module, intents).
    *   **Machine Context**: (# State Machine, # Failure Modes) for agent reasoning.

#### Phase 2: Workflow Hardening (Security & Atomic Neutrality)

*   [ ] **Validation**: Add `validateArbRequest` at workflow entry points.
*   [ ] **Parallel Exits**: Update `ExecuteSpotPerpExit` to use `dbos.RunWorkflow` for concurrent closures.
*   [ ] **Fill-Monitoring**: Add a polling loop with `dbos.Sleep` to `ExecuteLimitEntry` to ensure the Perp hedge matches actual Spot fill.

#### Phase 3: Risk & Margin Centralization

*   [ ] **Package Refactor**: Move `MarginSim` / `VME` to `internal/risk/margin`.
*   [ ] **Validation Gates**: Add margin health checks between rebalancing legs in the Portfolio loop.

#### Phase 4: System Guide & Deployment

*   [ ] **Sync**: Ensure `factory.go` propagates gRPC security fields.
*   [ ] **Final Audit**: Verify all PR #7 and PR #8 findings (identity checks) are functional.

## Acceptance Criteria

*   [ ] **Atomic Neutrality**: Arb engine correctly hedges partial fills without manual intervention.
*   [ ] **Safe Emergency**: Exit failures in one leg do not block closure attempts on the other.
*   [ ] **Agent Composable**: New workflows and docs are discoverable and usable by autonomous agents.
*   [ ] **Clean History**: Commit authorship followscorrected standards (`Tommy K`).

## References

*   **Learning: Blocking Mutex**: `docs/solutions/performance-issues/blocking-mutex-io-freeze.md`
*   **Learning: Atomic Neutrality**: `docs/solutions/performance-issues/arbitrage-strategy-execution-optimization-20260201.md`
*   **Design Spec (026)**: `market_maker/docs/specs/arbitrage_bot_design.md`
