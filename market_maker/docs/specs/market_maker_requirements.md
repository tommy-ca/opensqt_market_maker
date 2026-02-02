# Market Maker Bot - Functional Requirements

## 1. Strategy: Grid Trading
The `market_maker` bot implements a **Grid Trading** strategy to provide liquidity and capture the bid-ask spread.

### 1.1 Dynamic Grid Logic
- **Requirement**: The grid MUST track the market price (Trailing Grid).
- **Grid Center**: Calculated as `Round(CurrentPrice / Interval) * Interval`.
- **Active Window**:
  - Only slots within a configured window around the Grid Center are active for *new* order placement.
  - **Buy Window**: `[GridCenter - BuyWindow*Interval, GridCenter - Interval]`
  - **Sell Window**: `[GridCenter + Interval, GridCenter + SellWindow*Interval]`
- **Persistence**: Slots outside the window with existing positions MUST be retained to facilitate position closing (HODL until profitable).

### 1.2 Slot-Based Inventory Management
- **Requirement**: Trading MUST be organized into "Slots" (Grid Levels) with strict state locking.
- **States**:
  - `FREE`: Empty, available for new orders.
  - `PENDING`: Order request sent, awaiting `New` status from exchange.
  - `LOCKED`: Active order exists on exchange (Open Order).
  - `FILLED`: Position held at this level.
- **Constraint**: No operation can be performed on a slot unless it is `FREE` (for new orders) or `LOCKED` (for cancels/amends).
- **Locking**: Each slot must have its own mutex to allow concurrent processing of different price levels while protecting individual state.

### 1.3 Volatility Adaptation
- **Requirement**: The grid interval SHOULD adapt to market volatility.
- **Metric**: ATR (Average True Range) from `RiskMonitor`.
- **Logic**: `EffectiveInterval = BaseInterval * VolatilityScale * (CurrentATR / BaselineATR)`.
- **Goal**: Widen grid during high volatility to reduce risk/fees; tighten during low volatility to capture small moves.

### 1.4 Trend Following (Inventory Skew)
- **Requirement**: The grid placement SHOULD skew based on current inventory to manage risk.
- **Logic**:
  - If `Inventory > Target`: Skew grid DOWN (Sell lower, Buy lower).
  - If `Inventory < Target`: Skew grid UP (Sell higher, Buy higher).

## 2. Execution & Safety

### 2.1 Order Cleanup
- **Requirement**: Prevent accumulation of stale orders.
- **Logic**: If `OpenOrders > Threshold`, cancel the oldest orders that are furthest from the current price.

### 2.2 Risk Integration
- **Requirement**: Halting trading upon `RiskMonitor` triggers.
- **Action**: If `RiskMonitor.IsTriggered()` is true, cancel all BUY orders and pause new entries.

## 3. Configuration
- **Symbol**: Trading pair (e.g., `BTCUSDT`).
- **PriceInterval**: Base grid spacing.
- **OrderQuantity**: Size per grid level.
- **GridMode**: `long` (long-only bias), `short`, or `neutral`.
