# Market Maker Bot - Design Specification

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

## 3. State Management (`grid.SlotManager`)

### 3.1 Inventory Slots
- **Structure**: A map of `PriceLevel -> Slot`.
- **Slot States**:
  - `FREE`: Empty, available for new orders.
  - `PENDING`: Order request sent, awaiting exchange confirmation.
  - `LOCKED`: Active Open Order on exchange.
  - `FILLED`: Position held at this level.

### 3.2 State Synchronization
- **Startup**: Loads state from SQLite (Simple) or DBOS (Durable).
- **Reconciliation**: Periodically compares local slots with Exchange Open Orders to fix orphans or missing orders.

## 4. Execution Flow (`GridEngine`)

1. **Price Update**: Receive `PriceChange` from `PriceMonitor`.
2. **Risk Check**: If `RiskMonitor.IsTriggered()`:
   - Cancel all BUY orders.
   - Pause new entries.
3. **Calc Actions**: Call `Strategy.CalculateActions` with current price, ATR, and Slots.
4. **Execute**: 
   - `PlaceOrder`: If slot is FREE and in window.
   - `CancelOrder`: If order is out of window (trailing).
5. **Update State**: Mark slots as `PENDING` -> `LOCKED`.

## 5. Configuration

```yaml
trading:
  strategy_type: "grid"
  symbol: "BTCUSDT"
  price_interval: 10.0
  order_quantity: 0.001
  buy_window_size: 5
  sell_window_size: 5
  dynamic_interval: true
  volatility_scale: 1.0
  inventory_skew_factor: 0.001
```

## 6. Risk Controls
- **Circuit Breaker**: Halts trading if max consecutive losses are hit.
- **Order Cleanup**: Periodically cancels stale orders to free up slots.
- **Account Safety**: Verifies leverage and balance before startup.
