# Remote Stream Auto-Reconnect Spec

## Problem
The `RemoteExchange` gRPC client has **no reconnection logic** for streams (`StartOrderStream`, `StartPriceStream`, etc.). If the connection drops or the server restarts, the stream terminates, and the market maker stops receiving updates (blind trading).

## Solution Overview
Implement automatic reconnection with exponential backoff for all streaming methods in `RemoteExchange`. Upon successful reconnection, trigger an immediate state reconciliation to catch up on missed events.

## Detailed Design

### 1. Reconnection Logic
Wrap the stream consumption in a loop that:
1.  Establishes the stream.
2.  Consumes messages until error/EOF.
3.  Logs the error.
4.  Waits for a backoff duration (exponential: 1s, 2s, 4s... max 30s).
5.  Loops back to step 1.
6.  Resets backoff on successful connection (after X seconds of uptime or first message).

### 2. Reconciliation Trigger
When a stream reconnects, we might have missed `OrderUpdate` or `Position` events. We must trigger the `Reconciler` to sync state.
- **Challenge**: `RemoteExchange` is a low-level component and doesn't know about `Reconciler`.
- **Solution**: Add a callback hook or event channel `OnReconnect` to `RemoteExchange`.
- The `Engine` or `Reconciler` subscribes to this hook.

### 3. Implementation Plan

#### 3.1 Update `RemoteExchange`
Add `OnReconnect` callback.

```go
type RemoteExchange struct {
    // ...
    onReconnect func()
}

func (r *RemoteExchange) SetOnReconnectHandler(fn func()) {
    r.onReconnect = fn
}
```

Implement a generic `startStreamWithRetry` helper method to avoid code duplication across 5 stream methods.

```go
func (r *RemoteExchange) startStreamWithRetry(
    ctx context.Context,
    streamName string,
    connectFunc func() (interface{}, error), // returns the gRPC stream
    readFunc func(interface{}) (interface{}, error), // reads one message
    handleFunc func(interface{}), // processes the message
) error {
    go func() {
        backoff := time.Second
        maxBackoff := 30 * time.Second

        for {
            select {
            case <-ctx.Done():
                return
            default:
            }

            stream, err := connectFunc()
            if err != nil {
                r.logger.Error("Failed to connect stream", "stream", streamName, "error", err)
                time.Sleep(backoff)
                backoff = min(backoff*2, maxBackoff)
                continue
            }

            r.logger.Info("Stream connected", "stream", streamName)
            if r.onReconnect != nil {
                // Non-blocking trigger
                go r.onReconnect() 
            }
            backoff = time.Second // Reset backoff

            // Read loop
            for {
                msg, err := readFunc(stream)
                if err != nil {
                    r.logger.Error("Stream disconnected", "stream", streamName, "error", err)
                    break // Break read loop to reconnect
                }
                handleFunc(msg)
            }
        }
    }()
    return nil
}
```
*Note*: The above generic signature is tricky because gRPC streams have specific types (e.g. `ExchangeService_SubscribeOrdersClient`). Using `interface{}` loses type safety and might be hard to adapt to generated code `Recv()` methods.
*Better approach*: Use a closure for the connect and read logic, but keep the loop structure generic or duplicate slightly for type safety.
Given only 5 methods, mild duplication of the loop structure (or a closure-based helper) is acceptable.

#### 3.2 Update `StartOrderStream`, `StartPriceStream`, etc.
Refactor these methods to use the retry loop.

#### 3.3 Integration
- In `SimpleEngine`, when initializing `RemoteExchange`, set the `OnReconnect` handler to trigger `Reconciler.Reconcile(ctx)`.
- **Note**: `Reconciler` runs periodically. Triggering it immediately is an optimization/safety measure.

### 4. Testing
- **Unit Test**: Mock the gRPC client. Simulate `Recv()` returning `EOF` or `error` after N messages. Verify it calls `Subscribe...` again.
- **Integration Test**: (If possible) Restart the `exchange_connector` and verify `market_maker` reconnects.

## Acceptance Criteria
1.  `StartOrderStream` automatically reconnects after error.
2.  `StartPriceStream` automatically reconnects.
3.  Exponential backoff is applied.
4.  `OnReconnect` callback is fired upon reconnection.
