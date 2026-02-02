# Arbitrage Bot - Functional Requirements

## 1. Strategy: Funding Rate Arbitrage
The `arbitrage_bot` implements a delta-neutral strategy to capture the **Funding Fee** spread between two exchanges or products.

### 1.1 Core Logic
- **Goal**: Earn the funding spread while maintaining zero price exposure.
- **Spread**:
  - *Cross-Exchange*: `FundingRate(Short Leg) - FundingRate(Long Leg)`.
  - *Same-Exchange*: `FundingRate(Short Leg)` (Spot-Perp).
- **Entry Trigger**: `(FundingRate_Short - FundingRate_Long) * 3 * 365 > Min_Spread_APR`.
- **Exit Trigger**: `Current_Spread_APR < Exit_Spread_APR`.

### 1.2 State Management (Legs)
- **Requirement**: The bot must track positions across two separate exchange adapters.
- **Leg Manager**:
  - Maintains `LongLeg` (Exchange A) and `ShortLeg` (Exchange B).
  - Ensures **Delta Neutrality**: `Abs(Size_A - Size_B) < Threshold`.
  - **Synchronization**: Automatically recovers existing positions from exchanges on startup.

### 1.3 Execution Flow
- **Atomic Entry**:
  1. Place Order on Leg A (Spot Buy).
  2. If Success: Place Order on Leg B (Perp Sell).
  3. If Leg B Fails: **Compensate** (Unwind Leg A immediately).
- **Atomic Exit**:
  1. Place Order on Leg B (Perp Buy/Close).
  2. Place Order on Leg A (Spot Sell/Close).

## 2. Safety & Risk Control

### 2.1 Liquidation Guard
- **Requirement**: Protect capital from liquidation on the leveraged leg (Perp).
- **Metric**: `Distance = Abs(LiquidationPrice - MarkPrice) / MarkPrice`.
- **Trigger**: If `Distance < LiquidationThreshold` (e.g., 10%), execute **Emergency Exit** immediately, bypassing spread checks.

### 2.2 Global Risk Pause
- **Requirement**: Pause new entries during market crashes.
- **Action**: Query shared `RiskMonitor`. If `IsTriggered()` is true, skip new entry signals.

## 3. Universe Selection
- **Requirement**: Automatically scan the market for the best opportunities.
- **UniverseSelector**:
  - Scans available pairs on configured exchanges.
  - **Liquidity Filter**: Must filter symbols by 24h Quote Volume.
  - **Liquidity Intersection**: For Spot-Perp, BOTH legs must meet the minimum volume threshold.
  - **Open Interest Check**: Prioritize symbols with high Open Interest relative to their 24h volume (indicates sustained interest).
  - **Historical Analysis**: Must analyze historical funding rates to filter for quality.
    - **Stability**: Exclude pairs with high volatility in funding rates.
    - **Positive Expectation**: Prioritize pairs with a high percentage of positive funding intervals in the last 30 days.
    - **Duration**: Identify how long a pair has maintained its current funding polarity.
    - **Regime Change detection**: Identify transition periods from positive to opposite polarity.
    - **Momentum**: Analyze if the funding rate is increasing or decreasing over recent intervals.
    - **Predicted Drift**: Compare current rate with predicted rate. Large downward drifts (e.g., Current=0.01%, Predicted=0.005%) should penalize the score.
  - **Basis Factor**: Analyze the Spot-Perp premium/discount. If Perp price is significantly below Spot (negative basis), it may signal an imminent funding drop.
  - **Volatility Filter**: Exclude symbols with excessive historical price volatility (e.g., > 10% daily std dev).
  - **Atomic Neutrality**: Entry/Exit workflows MUST ensure delta neutrality. If Leg 1 (Spot) partially fills, Leg 2 (Perp) MUST be adjusted to match Leg 1's `ExecutedQty`.
  - **Parallel Execution**: Entry/Exit legs SHOULD be fired concurrently where supported by the exchange (or via Goroutines) to minimize execution slippage.
  - **Basis Stop (BaR)**: The system MUST support an emergency exit if the Spot-Perp basis remains negative for $>N$ consecutive intervals, indicating "Toxic Funding" regime change.
  - Updates the target symbol for the `ArbEngine`.

## 4. Configuration
- **Symbol**: Target pair (e.g., `BTCUSDT`).
- **SpotExchange**: Exchange for Long Leg.
- **PerpExchange**: Exchange for Short Leg.
- **MinSpreadAPR**: Entry threshold (e.g., 0.10).
- **ExitSpreadAPR**: Exit threshold (e.g., 0.01).
- **LiquidityThreshold24h**: Minimum 24h Quote Volume (in USDT) required for both legs (e.g., 10,000,000).
- **LiquidationThreshold**: Distance to liquidation limit (e.g., 0.10).
