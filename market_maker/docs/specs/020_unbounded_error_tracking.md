# Unbounded Error Tracking Fix Spec

## Problem
The `OrderExecutor` appends error timestamps to a slice without limit (`oe.errorTimestamps = append(...)`). In high-frequency error scenarios (e.g., rate limits), this causes unbounded memory growth and inefficient cleanup.

## Solution: Ring Buffer
Implement a fixed-size ring buffer (circular buffer) to store error timestamps. This ensures memory usage is bounded (O(1)) and insertion is fast.

## Design

### `OrderExecutor` Struct Update
```go
type OrderExecutor struct {
    // ...
    errorTimestamps []time.Time
    errorIndex      int
    errorCapacity   int
    errorMu         sync.Mutex
}
```

### `recordError` Method
```go
func (oe *OrderExecutor) recordError() {
    oe.errorMu.Lock()
    defer oe.errorMu.Unlock()

    if oe.errorCapacity == 0 {
        oe.errorCapacity = 1000 // Default fallback
    }

    if len(oe.errorTimestamps) < oe.errorCapacity {
        oe.errorTimestamps = append(oe.errorTimestamps, time.Now())
    } else {
        oe.errorTimestamps[oe.errorIndex] = time.Now()
        oe.errorIndex = (oe.errorIndex + 1) % oe.errorCapacity
    }
}
```

### `getErrorCount` Method
To count recent errors (e.g., last 5 mins), iterate through the buffer. Since it's not sorted in chronological order (it wraps around), we must check all elements. With cap=1000, this is negligible.

```go
func (oe *OrderExecutor) getRecentErrorCount(duration time.Duration) int {
    oe.errorMu.Lock()
    defer oe.errorMu.Unlock()

    cutoff := time.Now().Add(-duration)
    count := 0
    for _, t := range oe.errorTimestamps {
        if t.After(cutoff) {
            count++
        }
    }
    return count
}
```

### Cleanup Ticker
Remove the background cleanup goroutine entirely, as the ring buffer is self-limiting.

## Verification
Create `internal/trading/order/executor_limit_test.go`:
1.  Initialize executor with small capacity (e.g., 10).
2.  Record 100 errors.
3.  Verify `len(errorTimestamps) == 10`.
4.  Verify recent count is correct.

## Acceptance Criteria
- Memory bounded to `errorCapacity`.
- No cleanup goroutine.
- Tests pass.
