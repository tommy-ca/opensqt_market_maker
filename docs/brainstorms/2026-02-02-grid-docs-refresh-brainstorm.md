# Brainstorm: Grid Trading Documentation & Architecture Refresh

**Date:** 2026-02-02
**Topic:** Grid Trading System Overhaul (Documentation & Roadmap)
**Status:** Approved

## 1. What We're Building
A comprehensive refresh of the Grid Trading system's documentation and architectural roadmap. The goal is to bring Grid Trading up to the same standard of documentation and reliability as the Funding Arbitrage system.

We will create a modular documentation suite:
1.  **Grid Strategy Design Spec** (`market_maker/docs/specs/grid_strategy_design.md`): Focused on the "Why" and the theory (ATR scaling, skew, neutral mode).
2.  **Slot Manager Technical Reference** (`market_maker/docs/specs/slot_manager_reference.md`): Detailed state machine and protobuf data model documentation.
3.  **Durable Grid Roadmap** (`market_maker/docs/specs/durable_grid_roadmap.md`): The plan for migrating to DBOS-based durable execution.

## 2. Why This Approach?
*   **Consistency**: Matches the established pattern for the Funding Arbitrage system (Spec 026).
*   **Clarity**: Separates complex strategy theory from low-level state machine mechanics.
*   **Path to Production**: Provides a clear, documented path for the architectural migration to Durable Execution (DBOS), which is essential for production reliability.
*   **Deduplication**: Helps resolve the code duplication between `strategy/grid.go` and `grid/strategy.go` by first defining the desired behavior.

## 3. Key Decisions
*   **Modular Docs**: Use separate files for Theory, Reference, and Roadmap.
*   **Source of Truth**: Protobuf definitions in `api/proto/` remain the source of truth for data models.
*   **Migration Goal**: Grid Trading will eventually move to the same DBOS workflow model as Arbitrage to ensure atomicity and crash-resilience.

## 4. Open Questions
*   *Implementation detail*: Should the refactor of duplicated logic happen *before* or *during* the migration to the Durable Engine? (Likely during, to avoid double work).
*   *Integration*: How will the `RiskMonitor` dependencies be handled in the "clean" version of the strategy?

## 5. Next Steps
Run `/workflows:plan` to execute the documentation updates and define the technical requirements for the Durable Grid migration.
