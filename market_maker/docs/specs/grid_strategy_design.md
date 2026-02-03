# Grid Strategy Design Specification

## Overview
The Grid Strategy is a high-frequency trading logic that provides liquidity by placing a ladder of limit orders around a reference price. It aims to capture the bid-ask spread and profit from mean-reversion in sideways or oscillating markets.

## Strategy Narrative

### 1. Dynamic Grid (Trailing)
The strategy maintains a set of "Slots" (price levels). It calculates an **Anchor Price** (usually the initial price) and an **Effective Interval**.
The grid center is periodically recalculated based on the current market price, but snapped to the nearest grid line defined by the anchor. This ensures the grid "trails" the market while maintaining stable, predictable price levels.

### 2. Volatility Adaptation (ATR Scaling)
The price interval between grid levels is not fixed. It scales dynamically based on the **Average True Range (ATR)**.
- **Low Volatility**: Tighter grid to capture small fluctuations.
- **High Volatility**: Wider grid to reduce the risk of being "run over" by fast moves and to minimize fee drag.

### 3. Inventory Skew (Trend Following)
To manage inventory risk, the strategy applies a **Skew** to the perceived current price.
- If **Inventory is High (Long)**: The strategy skews the reference price **Down**, which causes the grid to shift down. This results in buying lower and selling lower (offloading).
- If **Inventory is Low (Short)**: The strategy skews the reference price **Up**.

### 4. Target-State Reconciliation
Unlike imperative strategies that issue "Buy" or "Cancel" commands, this strategy is **Declarative**.
It calculates the **Ideal State** (which positions should be held and which orders should be active) and returns it as a `TargetState`. The Engine is then responsible for computing the delta between reality and this target, and executing the necessary actions to converge.

## Machine Context

### State Machine
- **FREE**: Level is empty, no position, no order.
- **LOCKED**: An order is currently active at this level.
- **FILLED**: The level has an active position (captured liquidity).

### Failure Modes & Recovery
- **Partial Fills**: Handled naturally by the Reconciler. If a slot is partially filled, the Reconciler sees the missing quantity relative to the target and issues a corrective order.
- **Network Timeouts**: DBOS workflows ensure that order placement and state updates are atomic. If a crash occurs mid-execution, the workflow resumes and either completes the placement or marks the slot as FREE for the next reconciliation cycle.
- **Drift**: Periodic reconciliation against exchange-level data ensures that any internal state drift is corrected.

## Configuration Parameters
| Parameter | Description |
| :--- | :--- |
| `PriceInterval` | Base distance between grid levels. |
| `OrderQuantity` | Quantity per level. |
| `BuyWindowSize` | Number of active buy levels below current price. |
| `SellWindowSize` | Number of active sell levels above current price. |
| `VolatilityScale` | Multiplier for ATR-based interval scaling. |
| `InventorySkewFactor`| Factor for shifting grid based on net exposure. |
