---
status: resolved
priority: p1
issue_id: 010
tags: [code-review, data-integrity, critical, error-recovery]
dependencies: []
---

# Remote Exchange Stream Error Recovery Without State Validation

## Problem Statement

When order streams fail in `RemoteExchange`, the goroutine logs an error and exits permanently. There is **no reconnection logic** and **no mechanism to recover missed updates**. Orders that fill during disconnection are never received.

**Impact**:
- Permanent stream disconnection on network hiccup
- Fills lost during downtime
- Position manager thinks orders still open
- Reconciler may eventually detect (if it runs)
- **Trading blind** until manual restart

## Findings

**Location**: `internal/exchange/remote.go:235-257`

```go
// NO MISSED UPDATE RECOVERY
func (r *RemoteExchange) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
    stream, err := r.client.SubscribeOrders(ctx, &pb.SubscribeOrdersRequest{})
    if err != nil {
        return err
    }

    go func() {
        for {
            update, err := stream.Recv()
            if err == io.EOF {
                return  // STREAM DIES, NEVER RECONNECTS
            }
            if err != nil {
                r.logger.Error("Remote order stream received error", "error", err)
                return  // STREAM DIES, UPDATES LOST
            }

            callback(update)
        }
    }()

    return nil
}
```

**From Data Integrity Guardian Agent**:

**Data Loss Scenario**:
1. Order stream active, receiving updates
2. Network hiccup → stream error
3. Goroutine exits with error log
4. Orders fill during disconnection
5. Stream never reconnects (no retry logic)
6. **Fills are never received**
7. Position manager thinks orders still open
8. Reconciler eventually detects mismatch (if it runs)

## Proposed Solutions

### Option 1: Auto-Reconnect with Exponential Backoff (Recommended)
**Effort**: 4-5 hours
**Risk**: Low
**Pros**:
- Automatic recovery
- Standard pattern
- Proven reliability

**Cons**:
- Need to trigger reconciliation on reconnect

**Implementation**:
```go
func (r *RemoteExchange) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
    go func() {
        backoff := time.Second
        maxBackoff := 30 * time.Second

        for {
            select {
            case <-ctx.Done():
                return
            default:
            }

            // Establish stream
            stream, err := r.client.SubscribeOrders(ctx, &pb.SubscribeOrdersRequest{})
            if err != nil {
                r.logger.Error("Failed to subscribe to orders, retrying...", "error", err, "backoff", backoff)
                time.Sleep(backoff)
                backoff = min(backoff*2, maxBackoff)
                continue
            }

            r.logger.Info("Order stream connected")
            backoff = time.Second  // Reset on success

            // TRIGGER RECONCILIATION on reconnect to catch missed updates
            r.triggerReconciliation()

            // Read loop
            for {
                update, err := stream.Recv()
                if err == io.EOF || err != nil {
                    r.logger.Error("Order stream failed, reconnecting...", "error", err)
                    break  // Reconnect
                }

                callback(update)
            }
        }
    }()

    return nil
}

func (r *RemoteExchange) triggerReconciliation() {
    // Signal to reconciler that stream reconnected
    // Reconciler should compare local vs exchange state
}
```

### Option 2: Persistent Message Queue
**Effort**: 8-10 hours
**Risk**: Medium
**Pros**:
- No message loss
- Can replay missed updates

**Cons**:
- Complex infrastructure
- Requires message queue (Redis, NATS)

### Option 3: Checkpoint-Based Recovery
**Effort**: 6-8 hours
**Risk**: Medium
**Pros**:
- Resume from last known state
- Explicit recovery protocol

**Cons**:
- Need server-side support
- More complex client logic

## Recommended Action

**Option 1** - Standard pattern, works with existing infrastructure, just needs reconciliation trigger.

## Technical Details

### Affected Files
- `internal/exchange/remote.go`
  - Line 235: `StartOrderStream`
  - Line 259: `StartPriceStream`
  - Line 283: `StartKlineStream`
  - Line 307: `StartAccountStream`
  - Line 331: `StartPositionStream`

**All 5 stream methods** have the same issue.

### Reconciliation Integration
```go
// Add to RemoteExchange
type RemoteExchange struct {
    // ... existing fields
    reconciler *risk.Reconciler  // Optional reconciler
}

func (r *RemoteExchange) SetReconciler(reconciler *risk.Reconciler) {
    r.reconciler = reconciler
}

func (r *RemoteExchange) triggerReconciliation() {
    if r.reconciler != nil {
        r.reconciler.TriggerImmediateReconciliation()
    }
}
```

### Monitoring
- Add metric: `stream_reconnections_total` (counter)
- Add metric: `stream_uptime_seconds` (gauge)
- Alert on frequent reconnections (>10/hour)

## Acceptance Criteria

- [ ] Stream auto-reconnects on error
- [ ] Exponential backoff prevents thundering herd
- [ ] Reconciliation triggered after reconnect
- [ ] No lost fills in reconnection test
- [ ] Network partition test: disconnect for 10s, verify recovery
- [ ] All 5 stream methods have reconnection logic
- [ ] Metrics track reconnection count
- [ ] Integration tests pass

## Work Log

**2026-01-22**: Critical reliability issue. Production systems must handle network failures gracefully.

## Resources

- gRPC Retry Guide: https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md
- Data Integrity Review: See agent output above
- Exponential Backoff: https://en.wikipedia.org/wiki/Exponential_backoff
