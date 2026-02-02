---
title: "Blocking Mutex During I/O in Arbitrage Engine"
date: 2026-02-01
status: resolved
severity: critical
category: performance-issues
tags: [concurrency, mutex, go, performance, anti-pattern]
related_issues:
  - "023-completed-p1-blocking-mutex-during-io.md"
  - "docs/solutions/performance-issues/arbitrage-strategy-execution-optimization-20260201.md"
---

# Blocking Mutex During I/O

## Problem Statement

The `ArbitrageEngine` was experiencing "freezes" during critical network updates. Processing `OnFundingUpdate` or `OnAccountUpdate` events would block the entire engine, causing it to miss high-frequency `OnPriceUpdate` ticks. This made the bot unresponsive to market moves while it was fetching funding rates or placing orders.

### Symptoms
- Engine becomes unresponsive for 100ms to several seconds.
- `OnPriceUpdate` ticks are skipped or processed with high latency.
- "Internal Latency" spikes correlate with Funding or Account update events.
- Goroutine dumps show multiple routines blocked on `e.mu.Lock()` while one routine is waiting on network I/O.

## Investigation & Findings

### Root Cause Analysis
The engine's main mutex (`e.mu`) was being held throughout the *entire duration* of `OnFundingUpdate`. This included blocking I/O calls within `executeExit` and `executeEntry`:

1.  **Lock Acquisition**: `OnFundingUpdate` acquired `e.mu.Lock()`.
2.  **Blocking I/O**: Inside the critical section, it called `legManager.SyncState` (REST API fetch) and `executor.Execute` (Order Placement).
3.  **Contention**: During these slow network operations (100ms+), the lock remained held.
4.  **Blockage**: Other goroutines attempting to update prices via `OnPriceUpdate` (which takes microseconds) were blocked waiting for `e.mu`.

### Impact Code (Before)

```go
// market_maker/internal/engine/arbengine/engine.go

func (e *ArbitrageEngine) OnFundingUpdate(ctx context.Context, update *pb.FundingUpdate) error {
    e.mu.Lock() // <--- Lock acquired here
    defer e.mu.Unlock()

    // ... logic ...

    // CRITICAL ANTI-PATTERN: Blocking I/O inside the lock!
    // This blocks the entire engine while waiting for the exchange response.
    if err := e.executeEntry(ctx, true, update.NextFundingTime); err != nil {
        return err
    }
    
    return nil
}
```

## Solution

The fix involved implementing a **Fine-Grained Locking** strategy with an `isExecuting` state flag.

### 1. `isExecuting` Flag
Introduced an `isExecuting` boolean field to the engine struct. This flag marks when a long-running operation is in progress, allowing us to prevent re-entry logic without holding the mutex for the entire duration.

### 2. Lock Release Pattern
Refactored the handler to **release the mutex** specifically before entering the blocking execution methods and **re-acquire it** immediately after they return to safely update state.

### Corrected Code (After)

```go
// market_maker/internal/engine/arbengine/engine.go

func (e *ArbitrageEngine) OnFundingUpdate(ctx context.Context, update *pb.FundingUpdate) error {
    e.mu.Lock()
    
    // Check execution state (Fast)
    if e.isExecuting {
        e.mu.Unlock()
        return nil
    }

    // ... non-blocking logic ...

    if action == arbitrage.ActionEntryPositive {
        // 1. Mark as executing (Protected by lock)
        e.isExecuting = true
        
        // 2. RELEASE LOCK before blocking I/O
        e.mu.Unlock()
        
        // 3. Perform Blocking I/O (No Lock Held)
        err := e.executeEntry(ctx, true, update.NextFundingTime)
        
        // 4. Re-acquire lock to update state
        e.mu.Lock()
        e.isExecuting = false
        if err == nil {
            e.lastNextFundingTime = update.NextFundingTime
        }
        e.mu.Unlock()
        return err
    }
    
    e.mu.Unlock()
    return nil
}
```

## Prevention & Best Practices

To prevent this concurrency anti-pattern in future components:

### 1. "Compute, Lock, Update" Pattern
*   **Compute (Pre-I/O)**: Fetch necessary data or prepare payloads *before* acquiring the lock.
*   **Lock**: Enter critical section only for state checks and updates.
*   **Update**: If I/O is required based on the state, release the lock, perform I/O, then re-lock to update the result.

### 2. Detection Strategies
*   **Latency Metrics**: Instrument `Lock()` duration. If `time.Since(start_lock)` > 1ms, trigger an alert.
*   **Blocking Profile**: Use `runtime/pprof` blocking profile to visualize goroutines waiting on mutexes.
*   **Code Review Rule**: "No `http.Client` or `grpc.Client` calls inside a `defer mu.Unlock()` block."

### 3. Test Case for Verification
Create a test with a Mock Exchange that simulates high latency (e.g., `time.Sleep(100ms)`). Verify that `OnPriceUpdate` (running in a separate goroutine) can still process updates while the slow I/O is running.

```go
// Verification Pseudo-code
func TestEngineResponsiveness(t *testing.T) {
    eng := NewEngine()
    
    // Start Slow I/O
    go eng.OnFundingUpdate(slowContext)
    
    // Ensure Fast I/O is not blocked
    start := time.Now()
    eng.OnPriceUpdate(fastContext)
    if time.Since(start) > 5*time.Millisecond {
        t.Fatal("Price update blocked by Funding update I/O!")
    }
}
```
