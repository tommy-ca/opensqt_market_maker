# Brainstorm: Grid Trading Documentation Overhaul

**Date:** 2026-02-02
**Topic:** Grid Trading System Documentation Refresh
**Status:** Approved

## 1. What We're Building
A comprehensive documentation suite for the Grid Trading system, reflecting the current "Dual-Engine" architecture, Protobuf data models, and operational realities.

The documentation will be split into three focused files:
1.  **Architecture & Design** (`market_maker/docs/specs/market_maker_design.md`): The "Why" and high-level "How".
2.  **Technical Reference** (`market_maker/docs/specs/technical_reference.md`): The "What" (Structs, Protos, Interfaces).
3.  **Operations Guide** (`market_maker/docs/specs/operations_guide.md`): The "How-To" (Config, Recovery, Monitoring).

## 2. Why This Approach?
*   **Separation of Concerns**: Splitting the "Theory" from the "Code Details" prevents the design doc from becoming outdated quickly. Technical references can be generated or linked to code, while design principles remain stable.
*   **Operational Focus**: A dedicated Ops Guide addresses the "black box" problem, giving operators confidence to intervene during edge cases (stuck slots, risk triggers) without wading through architectural diagrams.
*   **Reflection of Reality**: The current docs do not fully capture the "Simple" vs "Durable" (DBOS) engine split, which is a core architectural feature.

## 3. Component Breakdown

### A. Design Specification (`market_maker_design.md`)
*   **Strategy Logic**:
    *   **Dynamic Grid**: How `PriceInterval` adapts to ATR (Volatility).
    *   **Inventory Skew**: How the `CenterPrice` shifts to manage inventory risk.
    *   **Trend Following**: Integration with `TrendFollowingStrategy` (if applicable).
*   **Architecture**:
    *   **Dual-Engine Pattern**: Explanation of the `Engine` interface and its two implementations (`Simple` for speed/testing, `Durable` for production safety).
    *   **Event Loop**: Visualizing the `PriceUpdate` -> `RiskCheck` -> `Strategy` -> `Execution` flow.

### B. Technical Reference (`technical_reference.md`)
*   **Data Models (Protobufs)**:
    *   `InventorySlot`: Detailed explanation of fields (`slot_status`, `position_status`) and lifecycle.
    *   `State`: Global state structure (`last_price`, `checksum`).
*   **Interfaces**:
    *   `Engine`: Core methods (`Start`, `Stop`, `OnPriceUpdate`).
    *   `Store`: Persistence abstraction (`Load`, `Save`).
*   **Durable Engine Internals**:
    *   DBOS Integration: How state is checkpointed and recovered.

### C. Operations Guide (`operations_guide.md`)
*   **Configuration**:
    *   Parameter reference (`VolatilityScale`, `SkewFactor`, `GridSize`).
    *   Recommended starting values for different asset classes.
*   **Monitoring**:
    *   Key metrics: `realized_pnl`, `unrealized_pnl`, `inventory_skew`, `active_slots`.
*   **Runbooks**:
    *   **Risk Mode Triggered**: What happens, how to reset.
    *   **Stuck/Orphan Slots**: How to identify and manually clear "LOCKED" slots that missed order updates.
    *   **Graceful Shutdown**: Ensuring state is saved before exit.

## 4. Key Decisions
*   **Modular Docs**: We will NOT attempt to maintain a single massive document.
*   **Protobuf as Truth**: The Technical Reference will treat the `.proto` files as the source of truth for data models.
*   **Ops Guide**: We will explicitly document "Risk Mode" behavior, which is often a source of confusion.

## 5. Open Questions
*   *Resolved during brainstorming*: The scope includes all three document types.
*   *Implementation detail*: We need to ensure the `Simple` engine docs don't get confused with `Durable` ones—they should be clearly marked as "Development/Backtest" vs "Production".

## 6. Next Steps
Run `/workflows:plan` to execute the documentation update. The plan should include steps to:
1.  Read the current `market_maker_design.md`.
2.  Draft the new `technical_reference.md` by inspecting code.
3.  Draft the `operations_guide.md` based on the `RiskMonitor` and `Config` code.
4.  Refactor `market_maker_design.md` to remove low-level details and focus on strategy.
