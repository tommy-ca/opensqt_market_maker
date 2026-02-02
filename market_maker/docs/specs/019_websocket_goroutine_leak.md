# WebSocket Goroutine Leak Fix Spec

## Problem
The `WebSocket` client creates a `heartbeat` goroutine for each connection attempt. This goroutine is **not tracked** by the `sync.WaitGroup` used in `Stop()`.
When `Stop()` is called:
1.  It cancels the context.
2.  It waits for `runLoop` to exit.
3.  It returns.

However, the `heartbeat` goroutine (which runs concurrently with `readLoop` inside `runLoop`) might still be running or waking up from `time.Sleep`/`ticker`. This constitutes a goroutine leak during shutdown, and potentially during reconnection if not managed carefully (though `heartbeatCancel` is called).

Specifically during shutdown: `Stop()` returns before `heartbeat` has finished cleanup.

## Solution
1.  In `runLoop`, before spawning `go c.heartbeat(...)`, increment `c.wg.Add(1)`.
2.  In `heartbeat`, add `defer c.wg.Done()`.

This ensures `Stop()` blocks until the heartbeat goroutine has actually exited.

## Implementation Details

### `pkg/websocket/client.go`

```go
func (c *Client) runLoop() {
    defer c.wg.Done()

    for {
        // ... connect ...

        // Start heartbeat if interval > 0
        heartbeatCtx, heartbeatCancel := context.WithCancel(c.ctx)
        if pingInterval > 0 {
            c.wg.Add(1) // Track heartbeat
            go c.heartbeat(heartbeatCtx, heartbeatCancel)
        }

        c.readLoop()
        heartbeatCancel() // Signal heartbeat to stop
        // ...
    }
}

func (c *Client) heartbeat(ctx context.Context, cancel context.CancelFunc) {
    defer c.wg.Done() // Signal completion
    // ...
}
```

## Testing
Create a test that:
1.  Starts the client (mocking the server).
2.  Ensures connection and heartbeat start.
3.  Calls `Stop()`.
4.  Checks `runtime.NumGoroutine()` before and after to ensure count returns to baseline.
5.  Without the fix, `Stop()` returns early, and `NumGoroutine` might be higher (or flaky).
6.  Actually, simpler: Inspect code or use a channel to verify execution order if `runtime.NumGoroutine` is noisy.
    - But `runtime.NumGoroutine` is standard for leak tests.

**Alternative Verification**:
Modify `heartbeat` to sleep briefly on exit. If `Stop()` returns immediately, the test finishes while heartbeat is sleeping -> leak. If `Stop()` waits, the test takes longer.

## Acceptance Criteria
- `Stop()` waits for `heartbeat` goroutine to exit.
- No goroutines leaked after `Stop()`.
