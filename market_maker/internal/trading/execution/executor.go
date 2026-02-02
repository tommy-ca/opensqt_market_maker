package execution

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
)

// Step defines a single operation and its compensation
type Step struct {
	Exchange   string
	Request    *pb.PlaceOrderRequest
	Compensate *pb.PlaceOrderRequest
}

// SequenceExecutor executes steps sequentially
type SequenceExecutor struct {
	exchanges map[string]core.IExchange
	logger    core.ILogger
}

func NewSequenceExecutor(exchanges map[string]core.IExchange, logger core.ILogger) *SequenceExecutor {
	return &SequenceExecutor{
		exchanges: exchanges,
		logger:    logger.WithField("component", "sequence_executor"),
	}
}

type executedStep struct {
	Step  Step
	Order *pb.Order
}

func (e *SequenceExecutor) Execute(ctx context.Context, steps []Step) error {
	executed := make([]executedStep, 0)

	for _, step := range steps {
		ex := e.exchanges[step.Exchange]
		order, err := ex.PlaceOrder(ctx, step.Request)
		if err != nil {
			e.logger.Error("Step failed, initiating compensation", "exchange", step.Exchange, "error", err)
			e.compensate(ctx, executed)
			return err
		}
		executed = append(executed, executedStep{Step: step, Order: order})
	}
	return nil
}

func (e *SequenceExecutor) compensate(ctx context.Context, executed []executedStep) {
	compensateAll(ctx, e.exchanges, e.logger, executed)
}

// ParallelExecutor executes steps in parallel
type ParallelExecutor struct {
	exchanges map[string]core.IExchange
	logger    core.ILogger
}

func NewParallelExecutor(exchanges map[string]core.IExchange, logger core.ILogger) *ParallelExecutor {
	return &ParallelExecutor{
		exchanges: exchanges,
		logger:    logger.WithField("component", "parallel_executor"),
	}
}

func (e *ParallelExecutor) Execute(ctx context.Context, steps []Step) error {
	results := make(chan struct {
		exStep executedStep
		err    error
	}, len(steps))

	for _, step := range steps {
		go func(s Step) {
			ex := e.exchanges[s.Exchange]
			order, err := ex.PlaceOrder(ctx, s.Request)
			results <- struct {
				exStep executedStep
				err    error
			}{
				exStep: executedStep{Step: s, Order: order},
				err:    err,
			}
		}(step)
	}

	executed := make([]executedStep, 0)
	var firstErr error

	for i := 0; i < len(steps); i++ {
		res := <-results
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
			}
			e.logger.Error("Step failed in parallel execution", "exchange", res.exStep.Step.Exchange, "error", res.err)
		} else {
			executed = append(executed, res.exStep)
		}
	}

	if firstErr != nil {
		e.logger.Error("One or more steps failed in parallel, initiating compensation")
		e.compensate(ctx, executed)
		return firstErr
	}

	return nil
}

func (e *ParallelExecutor) compensate(ctx context.Context, executed []executedStep) {
	compensateAll(ctx, e.exchanges, e.logger, executed)
}

func compensateAll(ctx context.Context, exchanges map[string]core.IExchange, logger core.ILogger, executed []executedStep) {
	// Compensate in reverse order
	for i := len(executed) - 1; i >= 0; i-- {
		exStep := executed[i]
		if exStep.Step.Compensate == nil {
			continue
		}

		// Adjust compensation quantity to executed quantity if available
		qty := exStep.Step.Compensate.Quantity
		if exStep.Order != nil && exStep.Order.ExecutedQty != nil {
			execQty := pbu.ToGoDecimal(exStep.Order.ExecutedQty)
			if !execQty.IsZero() {
				qty = pbu.FromGoDecimal(execQty)
			}
		}

		// Skip if nothing filled
		if pbu.ToGoDecimal(qty).IsZero() {
			continue
		}

		ex := exchanges[exStep.Step.Exchange]
		orig := exStep.Step.Compensate
		req := &pb.PlaceOrderRequest{
			Symbol:        orig.Symbol,
			Side:          orig.Side,
			Type:          orig.Type,
			TimeInForce:   orig.TimeInForce,
			Quantity:      qty,
			Price:         orig.Price,
			ReduceOnly:    orig.ReduceOnly,
			PostOnly:      orig.PostOnly,
			ClientOrderId: orig.ClientOrderId,
			UseMargin:     orig.UseMargin,
		}

		_, err := ex.PlaceOrder(ctx, req)
		if err != nil {
			logger.Error("CRITICAL: Compensation failed", "exchange", exStep.Step.Exchange, "error", err)
		}
	}
}
