# Reconciliation Race Condition Fix Spec

## Problem
The `Reconciler` reads position slots directly from `SuperPositionManager.GetSlots()` which returns a map reference (wrapped or not, if the map itself is not a copy or if elements are pointers, modifying elements concurrently is unsafe).
Actually, `SuperPositionManager.GetSlots()` **already creates a shallow copy** of the map, but the **values** are pointers to `core.InventorySlot`.
The Reconciler then reads fields of `*InventorySlot` (e.g. `slot.OrderId`, `slot.Quantity`) without locking the individual slot's mutex.
Concurrently, `OnOrderUpdate` locks individual slots (via `handleOrderFilledLocked` etc) and modifies them.
This causes a data race.

## Solution: Deep Copy Snapshot
We need a **Deep Copy** of the slots for reconciliation. The `Reconciler` does not need to block the `PositionManager` for the duration of the API call to the exchange (which is slow). It should grab a consistent snapshot of the local state, then fetch exchange state, then compare.

*Wait, if we fetch snapshot at T1, and fetch exchange state at T2 (latency 200ms), the state might change in between.*
This is a fundamental property of reconciliation. We are comparing "State at T1" vs "Exchange State at T2".
If an update happens at T1.5:
- Local state at T1 has Old Value.
- Exchange at T2 has New Value.
- Reconciler sees mismatch.
- Correction logic (from #021) runs.
- Correction logic calls `ForceSync` or `CancelOrder`.
If `ForceSync` is called, it might overwrite the valid update at T1.5.

**However**, the race condition #023 specifically refers to the **Go Data Race** (crash/corruption reading memory), not the logical race. We must fix the Data Race first.

### Plan
1.  **Modify `GetSlots` or add `GetSnapshot` in `SuperPositionManager`**:
    `GetSnapshot` already exists!
    Let's check `internal/trading/position/manager.go`.

```go
func (spm *SuperPositionManager) GetSnapshot() *pb.PositionManagerSnapshot {
    spm.mu.RLock()
    defer spm.mu.RUnlock()

    slots := make(map[string]*pb.InventorySlot)
    for k, v := range spm.slots {
        // Deep copy the slot to avoid data races
        v.Mu.RLock()
        s := *v.InventorySlot
        v.Mu.RUnlock()
        slots[k] = &s
    }
    // ...
}
```
It seems `GetSnapshot` **already does a deep copy** (copying the protobuf struct value).
So, if `Reconciler` uses `GetSnapshot()`, it is safe.

Let's check what `Reconciler` uses.
`internal/risk/reconciler.go`:
```go
// 2. Get Local State
slots := r.positionManager.GetSlots()
```

And `GetSlots()` in `manager.go`:
```go
func (spm *SuperPositionManager) GetSlots() map[string]*core.InventorySlot {
    spm.mu.RLock()
    defer spm.mu.RUnlock()
    result := make(map[string]*core.InventorySlot)
    for k, v := range spm.slots {
        result[k] = v // Shallow copy of pointer!
    }
    return result
}
```
**VULNERABILITY CONFIRMED**: `GetSlots` returns a map of **pointers** to the *live* slots. `Reconciler` reads these pointers without locking `v.Mu`.

### Fix Design
1.  Update `Reconciler` to use `GetSnapshot()` instead of `GetSlots()`.
    - `GetSnapshot()` returns `*pb.InventorySlot` (protobuf struct), not `*core.InventorySlot` (wrapper with Mutex).
    - `Reconciler` logic needs to be adapted to work with `*pb.InventorySlot`.
2.  Alternatively, update `GetSlots()` to return deep copies, OR add `GetSlotsSnapshot()` that returns `map[string]core.InventorySlot` (values, not pointers) or `map[string]*core.InventorySlot` where pointers point to new copies.
    - Returning `core.InventorySlot` (value) is tricky because it contains a Mutex (must not be copied).
    - So we must return `*pb.InventorySlot` or a new struct without the mutex.

**Selected Approach**: Use `GetSnapshot()` in `Reconciler`. It returns `*pb.PositionManagerSnapshot` which contains `map[string]*pb.InventorySlot`. These are safe, disconnected protobuf messages.

**Wait**, `Reconciler` logic might depend on `core.InventorySlot` features?
Let's check `reconcileOrders` signature.
```go
func (r *Reconciler) reconcileOrders(ctx context.Context, slots map[string]*core.InventorySlot, exchangeOrders []*pb.Order)
```
It takes `map[string]*core.InventorySlot`.

**Refactoring Required**:
1.  Change `reconcileOrders` and `reconcilePositions` to accept `map[string]*pb.InventorySlot`.
2.  Update `Reconciler.Reconcile` to call `r.positionManager.GetSnapshot()` and pass the slots from there.

**Interface Update**:
`IPositionManager` already has `GetSnapshot()`. `GetSlots()` is also there. We might leave `GetSlots` for internal/testing use or strictly read-only if caller locks (but caller can't lock easily).
Actually, `GetSlots` is dangerous. Ideally we deprecate it or rename to `GetLiveSlotsUnsafe`.

### Step-by-Step Implementation
1.  Create `internal/risk/reconciler_race_test.go` to reproduce the race (using the logic from the todo).
2.  Modify `Reconciler` to use `GetSnapshot()` instead of `GetSlots()`.
3.  Update `reconcileOrders` and `reconcilePositions` signatures.
4.  Run tests to verify race is gone.

## Acceptance Criteria
- `go test -race` passes for the new reproduction test.
- `Reconciler` no longer calls `GetSlots()`.
- Code builds and passes existing tests.
