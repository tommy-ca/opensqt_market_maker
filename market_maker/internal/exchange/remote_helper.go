package exchange

import (
	"context"
	"io"
	"time"
)

// startStreamWithRetry starts a stream with automatic reconnection and exponential backoff
func (r *RemoteExchange) startStreamWithRetry(
	ctx context.Context,
	streamName string,
	connectFunc func(context.Context) (interface{}, error),
	readFunc func(interface{}) (interface{}, error),
	handleFunc func(interface{}),
) error {
	go func() {
		backoff := 1 * time.Second
		maxBackoff := 30 * time.Second

		for {
			select {
			case <-ctx.Done():
				r.logger.Info(streamName + " context cancelled, stopping")
				return
			default:
			}

			// Add authentication metadata to context
			authCtx := r.addAuthMetadata(ctx)

			// Establish stream
			stream, err := connectFunc(authCtx)
			if err != nil {
				r.logger.Error("Failed to connect stream, retrying...",
					"stream", streamName, "error", err, "backoff", backoff)
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				continue
			}

			r.logger.Info("Stream connected", "stream", streamName)
			backoff = 1 * time.Second // Reset on success

			// Trigger reconciliation on reconnect (if configured)
			r.triggerReconciliation()

			// Read loop
			for {
				msg, err := readFunc(stream)
				if err == io.EOF {
					r.logger.Warn("Stream closed by server (EOF), reconnecting...", "stream", streamName)
					break
				}
				if err != nil {
					r.logger.Error("Stream failed, reconnecting...", "stream", streamName, "error", err)
					break // Reconnect
				}

				handleFunc(msg)
			}
		}
	}()
	return nil
}
