# Grid Hardening Validation Specification

## Overview
This specification defines the validation scenarios for the Grid Trading Workflow Hardening (PR #15). It focuses on verifying critical concurrency fixes, regime filtering logic, and persistence reliability using declarative state assertions.

## Scenarios

### 1. Regime Filtering
**Goal:** Verify Strategy adheres to market regime constraints.

| Scenario | Given Regime | Action | Expectation |
| :--- | :--- | :--- | :--- |
| **Bull Trend** | `BULL_TREND` | Price Update | • `executor` receives **zero** `ORDER_SIDE_SELL` actions.<br>• `executor` receives `ORDER_SIDE_BUY` actions (if applicable).<br>• Log: `Regime Changed: RANGE -> BULL_TREND` |
| **High Volatility** | `HIGH_VOLATILITY` | Price Update | • `executor` receives **zero** new orders.<br>• Existing orders may be cancelled (Reduce Only). |
| **Time of Day** | `RANGE` | Clock = Weekend | • Strategy enters `OFF` or `REDUCE_ONLY`.<br>• No new orders placed despite valid regime. |

### 2. Coordinator Persistence (Recovery)
**Goal:** Verify `GridCoordinator` recovers exact state after a crash.

**Test Strategy:** "Kill & Restart" Pattern
1.  **Start Engine A**: Place orders, accumulate inventory.
2.  **Crash**: Stop Engine A, discard instance. Keep SQLite DB and Mock Exchange alive.
3.  **Ghost Fill**: Manually fill an order on Mock Exchange while engine is down.
4.  **Start Engine B**: Connect to same DB.

**Expectations:**
*   **State Restoration**: `LoadState` recovers `InventorySlot` map and `AnchorPrice` exactly.
*   **Reconciliation**: Engine B detects the ghost fill and updates `InventorySlot` to `FILLED`.
*   **Invariant**: `Wallet Balance + Inventory Value == Total Equity` (consistent before/after).

### 3. Risk Safety
**Goal:** Verify Risk Monitor triggers "Reduce Only" mode on volatility spikes.

| Trigger | Action | Expectation |
| :--- | :--- | :--- |
| **ATR Spike** | `MockRiskMonitor` emits high ATR | • `GridCoordinator` cancels all open `BUY` orders.<br>• Subsequent Price Updates trigger NO new `BUY` orders.<br>• Log: `Risk Monitor Triggered` |

### 4. Concurrency & Blocking I/O
**Goal:** Verify Coordinator does not freeze during slow I/O (Regression Test).

**Test Strategy:** Signal-and-Wait
1.  **Setup**: `BlockingExchange` that signals when inside `SubmitOrder`.
2.  **Trigger Slow**: Call `Rebalance` (async). Wait for `ready` signal.
3.  **Trigger Fast**: Call `OnPriceUpdate` (sync).
4.  **Assert**: `OnPriceUpdate` returns immediately (<5ms), proving lock release.
5.  **Cleanup**: Signal `finish` to let `Rebalance` complete.

## Verification Methods
*   **Stateful Fakes**: Use `SimulatedExchange` and `MockRegimeMonitor` (not stateless mocks).
*   **Declarative Assertions**: Verify `TargetState` convergence vs. imperative call counts.
*   **Observability**: Assert presence of structured logs and metric increments.
