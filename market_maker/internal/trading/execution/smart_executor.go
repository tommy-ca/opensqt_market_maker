package execution

import (
	"context"
	"fmt"
	"market_maker/internal/core"
)

// BatchCancelStep defines a batch cancel operation
type BatchCancelStep struct {
	Exchange string
	Symbol   string
	OrderIDs []int64
}

// SmartExecutor unifies sequential and parallel execution with batch support
type SmartExecutor struct {
	exchanges map[string]core.IExchange
	logger    core.ILogger
}

func NewSmartExecutor(exchanges map[string]core.IExchange, logger core.ILogger) *SmartExecutor {
	return &SmartExecutor{
		exchanges: exchanges,
		logger:    logger.WithField("component", "smart_executor"),
	}
}

// ExecuteSequence executes steps one by one, stopping on error and compensating
func (e *SmartExecutor) ExecuteSequence(ctx context.Context, steps []Step) error {
	seq := NewSequenceExecutor(e.exchanges, e.logger)
	return seq.Execute(ctx, steps)
}

// ExecuteParallel executes steps concurrently, compensating if any fail
func (e *SmartExecutor) ExecuteParallel(ctx context.Context, steps []Step) error {
	par := NewParallelExecutor(e.exchanges, e.logger)
	return par.Execute(ctx, steps)
}

// BatchCancel executes cancellations in parallel
func (e *SmartExecutor) BatchCancel(ctx context.Context, cancels []BatchCancelStep) error {
	// Simple parallel loop
	errCh := make(chan error, len(cancels))
	for _, c := range cancels {
		go func(step BatchCancelStep) {
			ex, ok := e.exchanges[step.Exchange]
			if !ok {
				errCh <- fmt.Errorf("exchange not found: %s", step.Exchange)
				return
			}
			errCh <- ex.BatchCancelOrders(ctx, step.Symbol, step.OrderIDs, false)
		}(c)
	}

	var lastErr error
	for i := 0; i < len(cancels); i++ {
		if err := <-errCh; err != nil {
			e.logger.Error("Batch cancel failed", "error", err)
			lastErr = err
		}
	}
	return lastErr
}
