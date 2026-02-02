# Market Maker Bot - Design Specification

## Documentation Map

*   **Concepts & Architecture**: This Document
*   **Technical Details**: [Technical Reference](technical_reference.md) (Protobufs, Interfaces, Workflows)
*   **Operational Guide**: [Operations Guide](operations_guide.md) (Config, Monitoring, Runbooks)

## 1. Overview
The `market_maker` binary runs a high-frequency grid trading strategy designed to provide liquidity on a specific symbol. It uses a **Dual-Engine** architecture (Simple/Durable) to manage inventory slots and capture the spread.

## 2. Strategy Logic (`grid.Strategy`)

### 2.1 Dynamic Grid (Trailing)
- **Concept**: The grid tracks the market price to ensure active orders are always close to the last traded price.
- **Grid Center**: Calculated as `Round(CurrentPrice / Interval) * Interval`.
- **Active Window**:
  - **Buy Window**: `[GridCenter - BuyWindow*Interval, GridCenter - Interval]`
  - **Sell Window**: `[GridCenter + Interval, GridCenter + SellWindow*Interval]`
- **Persistence**: Slots outside the window with existing positions are retained to facilitate position closing (HODL until profitable).

### 2.2 Volatility Adaptation (Dynamic Interval)
- **Goal**: Widen grid during high volatility to reduce risk/fees; tighten during low volatility to capture small moves.
- **Metric**: Uses **ATR (Average True Range)** from `RiskMonitor`.
- **Formula**: `EffectiveInterval = BaseInterval * VolatilityScale * (CurrentATR / BaselineATR)`.

### 2.3 Trend Following (Inventory Skew)
- **Goal**: Manage inventory risk by biasing the grid against the current position.
- **Logic**:
  - If `Inventory > Target`: Skew grid DOWN (Sell lower, Buy lower).
  - If `Inventory < Target`: Skew grid UP (Sell higher, Buy higher).
- **Formula**: `SkewedPrice = CurrentPrice * (1 - SkewFactor * InventoryRatio)`.

## 3. Architecture

### 3.1 Dual-Engine Pattern
The system defines an abstract `Engine` interface with two distinct implementations:

1.  **Simple Engine**:
    *   **Goal**: Speed and simplicity for Backtesting and Local Development.
    *   **Storage**: In-Memory or SQLite (WAL mode).
    *   **Execution**: Direct API calls.

2.  **Durable Engine**:
    *   **Goal**: Reliability and Correctness for Production.
    *   **Storage**: DBOS (Transactional Workflow Engine).
    *   **Execution**: Idempotent workflows with automatic retries and crash recovery.

See [Technical Reference](technical_reference.md) for implementation details.

### 3.2 Execution Flow (`GridEngine`)

The engine operates on an event loop driven by price updates:

1.  **Price Update**: Receive `PriceChange` from `PriceMonitor`.
2.  **Risk Check**: If `RiskMonitor.IsTriggered()`:
    - Cancel all BUY orders.
    - Pause new entries.
3.  **Calc Actions**: Call `Strategy.CalculateActions` with current price, ATR, and Slots.
4.  **Execute**:
    - `PlaceOrder`: If slot is FREE and in window.
    - `CancelOrder`: If order is out of window (trailing).
5.  **Update State**: Mark slots as `PENDING` -> `LOCKED`.

## 4. Risk Controls
- **Circuit Breaker**: Halts trading if max consecutive losses are hit.
- **Order Cleanup**: Periodically cancels stale orders to free up slots.
- **Account Safety**: Verifies leverage and balance before startup.

For emergency procedures, see the [Operations Guide](operations_guide.md).
