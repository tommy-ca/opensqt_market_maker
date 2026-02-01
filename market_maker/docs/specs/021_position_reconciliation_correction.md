# Position Reconciliation Correction Spec

## Problem
The `Reconciler` currently detects position mismatches between the local state (`SuperPositionManager`) and the exchange state (`Exchange.GetPosition`), but takes no corrective action. It only logs a warning. This allows divergence to persist, leading to incorrect trading decisions and risk limit violations.

## Solution Overview
Implement a hybrid correction mechanism:
1.  **Small Divergence (< 5%)**: Automatically correct the local position state to match the exchange.
2.  **Large Divergence (>= 5%)**: Open the Circuit Breaker to halt trading and alert operations.

## Detailed Design

### 1. PositionManager Interface Update
Add a `ForceSync` method to the `IPositionManager` interface (and `SuperPositionManager` implementation).

```go
// ForceSync forces the local position state to match the exchange state
ForceSync(ctx context.Context, symbol string, exchangeSize decimal.Decimal) error
```

**Implementation Details**:
- Lock the manager.
- Calculate the difference (`adjustment = exchangeSize - currentSize`).
- Update the `InventorySlot`s or internal aggregation to reflect the new size.
    - *Note*: Since `SuperPositionManager` uses a slot-based system, "updating position" is non-trivial. It might need to create a "synthetic" filled slot or adjust an existing one.
    - *Simpler Approach*: If `SuperPositionManager` tracks `netPosition` separately, update that. But it aggregates slots.
    - *Proposed Strategy*:
        - If `exchangeSize > localSize` (Longer): Find an empty slot or create a new one and mark it `FILLED` with the difference.
        - If `exchangeSize < localSize` (Shorter): Find `FILLED` slots and reduce their quantity or mark them `EMPTY` until the difference is met.
        - *Alternative*: If the system is "Net Position" based for risk but "Slot Based" for grid, we might just need to ensure the *Total* matches.
        - *Simplest Robust Strategy*: Nuke and Rebuild? No, that destroys PnL history.
        - *Selected Strategy*:
            - Log the offset.
            - Create a special "Reconciliation Adjustment" slot (or multiple) to bridge the gap.
            - Or, since this is a `SuperPositionManager` (Grid), maybe we just accept the `exchangeSize` as the source of truth for Risk, but for the Grid, we might need to reset.
            - *Refined Strategy*: For this spec, we will assume `ForceSync` effectively updates the *Total* position. How it does it internally (updating slots) is an implementation detail of `PositionManager`. Ideally, it adjusts the `PositionQty` of the most recent filled slots or adds a new one.

### 2. Reconciler Logic Update
Update `reconcilePositions` in `internal/risk/reconciler.go`.

```go
divergence := exchangeSize.Sub(localSize)
divergencePct := divergence.Div(exchangeSize.Abs().Add(decimal.NewFromFloat(0.0001))).Mul(decimal.NewFromInt(100)).Abs() // Add epsilon to avoid div by zero

if divergencePct.LessThan(decimal.NewFromInt(5)) {
    // Auto-correct
    r.positionManager.ForceSync(ctx, r.symbol, exchangeSize)
} else {
    // Halt
    r.circuitBreaker.Open(r.symbol, "large_position_divergence")
}
```

## Metrics
- `position_divergence_amount`: Gauge
- `position_corrections_total`: Counter (label: type=auto|manual)

## Acceptance Criteria
1.  **Small Divergence**: When local=100, exchange=101 (1%), `ForceSync` is called.
2.  **Large Divergence**: When local=100, exchange=200 (100%), `CircuitBreaker.Open` is called.
3.  **Zero Divergence**: No action.
