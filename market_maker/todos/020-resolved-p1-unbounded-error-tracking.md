---
status: completed
priority: p1
issue_id: 020
tags: [code-review, performance, critical, memory-leak, unbounded-growth]
dependencies: []
---

# Unbounded Error Timestamp Growth in Order Executor

## Problem Statement

**Location**: `internal/trading/order/executor.go:206`

Error timestamps are appended without bounds checking:

```go
oe.errorTimestamps = append(oe.errorTimestamps, time.Now())  // No bounds check
```

**Impact**:
- **Memory leak**: During rate limit errors, can grow to 6,000+ entries per hour
- **Memory growth**: 6,000 timestamps × 8 bytes = 48 KB/hour baseline
- **Cleanup inefficiency**: 5-minute cleanup window means recent errors never pruned
- **Scale**: Multiple order executors × multiple symbols = multiplicative growth

## Evidence

From Performance Oracle review:
> "The errorTimestamps slice grows unbounded. During high error rates (e.g., exchange rate limiting), this could append thousands of timestamps per minute without any cap."

## Root Cause Analysis

**Current implementation** (from Issue #006 resolution):
```go
type OrderExecutor struct {
    errorTimestamps []time.Time  // Unbounded slice
    errorMu         sync.Mutex
}

func (oe *OrderExecutor) recordError() {
    oe.errorMu.Lock()
    oe.errorTimestamps = append(oe.errorTimestamps, time.Now())  // NO LIMIT
    oe.errorMu.Unlock()
}
```

**Cleanup ticker** (runs every 1 minute):
- Prunes timestamps older than 5 minutes
- **BUT**: During sustained errors, cleanup can't keep up with append rate
- Example: 100 errors/second = 6,000 appends/minute vs 1 cleanup/minute

## Failure Scenario

1. Exchange returns 429 (rate limit exceeded)
2. Retry logic attempts order placement 100 times/second
3. Each attempt calls `recordError()`
4. Slice grows: 6,000 entries/minute
5. Cleanup runs every 60 seconds, removes entries older than 5 minutes
6. **Net growth**: ~30,000 entries (5 minutes × 6,000/minute)
7. Memory: 30,000 × 8 bytes = 240 KB per executor
8. With 10 symbols × 5 exchanges = 50 executors = **12 MB memory leak**

## Proposed Solution

### Option 1: Ring Buffer (Recommended)

**Effort**: 3-4 hours

```go
type OrderExecutor struct {
    errorTimestamps []time.Time
    errorIndex      int
    errorCapacity   int  // Max 1000 entries
    errorMu         sync.Mutex
}

func NewOrderExecutor(cfg *Config) *OrderExecutor {
    return &OrderExecutor{
        errorTimestamps: make([]time.Time, 0, 1000),
        errorCapacity:   1000,
    }
}

func (oe *OrderExecutor) recordError() {
    oe.errorMu.Lock()
    defer oe.errorMu.Unlock()

    if len(oe.errorTimestamps) < oe.errorCapacity {
        // Still growing, append normally
        oe.errorTimestamps = append(oe.errorTimestamps, time.Now())
    } else {
        // At capacity, overwrite oldest entry (ring buffer)
        oe.errorTimestamps[oe.errorIndex] = time.Now()
        oe.errorIndex = (oe.errorIndex + 1) % oe.errorCapacity
    }
}

func (oe *OrderExecutor) getRecentErrors() int {
    oe.errorMu.Lock()
    defer oe.errorMu.Unlock()

    cutoff := time.Now().Add(-5 * time.Minute)
    count := 0
    for _, t := range oe.errorTimestamps {
        if t.After(cutoff) {
            count++
        }
    }
    return count
}
```

**Benefits**:
- **Bounded memory**: Max 1,000 × 8 bytes = 8 KB (fixed)
- **Fast append**: O(1) operation, no slice growth
- **Simple cleanup**: No cleanup ticker needed
- **Accurate counting**: Recent errors still tracked correctly

### Option 2: Sliding Window with Cap

**Effort**: 2-3 hours

```go
const maxErrorTracking = 1000

func (oe *OrderExecutor) recordError() {
    oe.errorMu.Lock()
    defer oe.errorMu.Unlock()

    // Add new timestamp
    oe.errorTimestamps = append(oe.errorTimestamps, time.Now())

    // Enforce cap immediately
    if len(oe.errorTimestamps) > maxErrorTracking {
        // Keep most recent 1000 entries
        oe.errorTimestamps = oe.errorTimestamps[len(oe.errorTimestamps)-maxErrorTracking:]
    }
}
```

**Benefits**:
- **Simple implementation**: 5-line change
- **Bounded memory**: Max 8 KB
- **No cleanup ticker needed**: Self-limiting

**Trade-off**: Slice reallocation on every cap enforcement (less efficient than ring buffer)

## Recommended Action

**Implement Option 1** (ring buffer):
- More efficient for high-frequency errors
- Predictable memory usage
- Better performance characteristics

**Additional improvement**:
```go
// Make capacity configurable per executor
type Config struct {
    MaxErrorTracking int  // Default 1000, can increase for debugging
}
```

## Testing

**Memory leak test**:
```go
func TestErrorTrackingBounded(t *testing.T) {
    executor := NewOrderExecutor(cfg)

    // Simulate 10,000 errors
    for i := 0; i < 10000; i++ {
        executor.recordError()
    }

    // Verify bounded growth
    executor.errorMu.Lock()
    count := len(executor.errorTimestamps)
    executor.errorMu.Unlock()

    if count > 1000 {
        t.Errorf("Error tracking unbounded: %d entries", count)
    }
}
```

**High-frequency error test**:
```go
func BenchmarkRecordError(b *testing.B) {
    executor := NewOrderExecutor(cfg)
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            executor.recordError()
        }
    })
}
```

## Acceptance Criteria

- [x] Ring buffer implementation with configurable capacity
- [x] Memory usage bounded to 8 KB per executor
- [x] 10,000 error test shows no unbounded growth
- [x] Concurrent error recording test passes
- [x] Cleanup ticker removed (no longer needed)
- [x] All tests pass

## Resources

- Performance Oracle Report: Unbounded growth finding
- File: `internal/trading/order/executor.go`
- Related: Issue #006 (goroutine leak - already resolved)
