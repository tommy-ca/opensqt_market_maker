---
status: completed
priority: p1
issue_id: 019
tags: [code-review, performance, critical, goroutine-leak, memory-leak]
dependencies: []
---

# WebSocket Heartbeat Goroutine Leak

## Problem Statement

**Location**: `pkg/websocket/client.go:148-151`

Heartbeat goroutines were created but never tracked or cleaned up:

```go
heartbeatCtx, heartbeatCancel := context.WithCancel(c.ctx)
if pingInterval > 0 {
    go c.heartbeat(heartbeatCtx, heartbeatCancel)  // Was never tracked with WaitGroup
}
```

## Evidence

From Performance Oracle review:
> "The heartbeat goroutine is spawned but never added to a WaitGroup or tracked for shutdown. When the WebSocket client closes, this goroutine may continue running indefinitely."

## Root Cause Analysis

**Missing lifecycle management**:
1. Goroutine created in `Connect()` (now `runLoop()`) method
2. Context cancellation provides termination signal
3. **BUT**: No mechanism to wait for goroutine exit
4. **Result**: Shutdown returns before goroutine terminates

## Resolution

**Implemented WaitGroup tracking and graceful shutdown**:

1.  **WaitGroup Tracking**: Added `sync.WaitGroup` to `Client` struct to track all background goroutines (`runLoop` and `heartbeat`).
2.  **Graceful Stop**: Updated `Stop()` method to signal cancellation and wait for all goroutines to exit with a 5-second timeout.
3.  **Responsive Reconnection**: Updated `runLoop` to use interruptible sleeps (`select` with `time.After` and `ctx.Done()`) ensuring prompt shutdown even during reconnection backoff.
4.  **Heartbeat Management**: Ensured `heartbeat` goroutine is properly tracked and cancelled when connection is lost or client is stopped.

## Testing

**Goroutine leak test**:
Ran `TestGoroutineLeak` in `market_maker/pkg/websocket/client_leak_test.go` which verifies that goroutines are cleaned up after `Stop()`.

```bash
go test -v ./market_maker/pkg/websocket/...
```

Output:
```
=== RUN   TestGoroutineLeak
--- PASS: TestGoroutineLeak (0.37s)
PASS
```

## Affected Components

- `market_maker/pkg/websocket/client.go`

## Acceptance Criteria

- [x] WaitGroup added to Client struct
- [x] All goroutines tracked (heartbeat, runLoop)
- [x] Stop() waits for goroutine exit (with timeout)
- [x] Goroutine leak test passes
- [x] 100 connection stress test shows no goroutine growth after Stop()
- [x] All tests pass

## Resources

- Performance Oracle Report: Goroutine leak finding
- File: `pkg/websocket/client.go`
