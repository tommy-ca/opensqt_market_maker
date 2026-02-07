# Brainstorm: Static Compliance Check of Refactor

**Date:** 2026-02-03
**Topic:** Validating Market Maker & Arb Bot Refactor against Specs
**Status:** Approved

## 1. What We're Building
A **Compliance Report** that validates whether the recent "Engine Unification" and "Declarative Reconciliation" refactor actually meets the requirements defined in the design specifications.

We will produce:
1.  **Traceability Report**: A document (`docs/specs/compliance_report_2026_02_03.md`) mapping requirements to code implementation.
2.  **Gap Analysis**: Identifying any discrepancies where the code drifted from the design (or vice versa).
3.  **Remediation Plan**: A set of tasks to fix any found gaps.

## 2. Why This Approach?
*   **Verification**: We performed a complex refactor involving deletions and restores. We need to be 100% sure the codebase is consistent (e.g., `GridStrategy` actually implements the interface `GridEngine` calls).
*   **Documentation Integrity**: Specs often rot. This check ensures our docs (`market_maker_design.md`) are accurate reflections of the system.
*   **Safety**: Ensuring safety mechanisms (Circuit Breakers, Risk Monitors) are correctly wired in the new architecture.

## 3. Key Areas to Validate

### A. Grid Trading (Declarative Reconciliation)
*   **Requirement**: Strategy returns `TargetState` (Positions + Orders).
*   **Code Check**: Verify `internal/trading/grid/strategy.go` implements `CalculateTargetState`.
*   **Verification**: Verify `internal/engine/gridengine/engine.go` consumes `TargetState` and executes the delta.

### B. Funding Arbitrage (Execution Hardening)
*   **Requirement**: Atomic Entry (Spot IOC + Perp Hedge).
*   **Code Check**: Verify `internal/engine/arbengine/engine.go` uses `TIME_IN_FORCE_IOC` for entry legs.
*   **Requirement**: Parallel Exits.
*   **Code Check**: Verify `executeExit` launches concurrent goroutines or workflows.

### C. Architecture (Unification)
*   **Requirement**: Shared `PositionTracker` / `SuperPositionManager`.
*   **Code Check**: Verify both engines use the same state tracking component.
*   **Requirement**: Shared `SmartExecutor` logic (if applicable).

## 4. Open Questions
*   *Resolved*: We will use a manual trace approach rather than writing a new tool.
*   *Risk*: If `GridStrategy` still has `CalculateActions`, the engine build will fail. We must verify this file content explicitly.

## 5. Next Steps
Run `/workflows:plan` to execute the validation.
1.  Read `market_maker/docs/specs/grid_strategy_design.md`.
2.  Read `market_maker/internal/trading/grid/strategy.go` and `engine.go`.
3.  Read `market_maker/internal/engine/arbengine/engine.go`.
4.  Generate `docs/specs/compliance_report_2026_02_03.md`.
5.  If gaps are found, generate a `fix` plan.
