package concurrency

import (
	"fmt"
	"market_maker/internal/core"
	"sync"
	"time"

	"github.com/alitto/pond"
)

// PoolConfig holds configuration for a worker pool
type PoolConfig struct {
	Name        string
	MaxWorkers  int
	MaxCapacity int
	IdleTimeout time.Duration
	NonBlocking bool // If true, Submit() returns error instead of blocking when full
}

// WorkerPool wraps alitto/pond with monitoring and standardized config
type WorkerPool struct {
	pool   *pond.WorkerPool
	config PoolConfig
	logger core.ILogger
	mu     sync.RWMutex
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(cfg PoolConfig, logger core.ILogger) *WorkerPool {
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 10 // Safe default
	}
	if cfg.MaxCapacity <= 0 {
		cfg.MaxCapacity = 100 // Safe default
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 60 * time.Second
	}

	strategy := pond.Strategy(pond.Balanced()) // Balanced strategy is generally good

	pool := pond.New(
		cfg.MaxWorkers,
		cfg.MaxCapacity,
		pond.MinWorkers(1),
		pond.IdleTimeout(cfg.IdleTimeout),
		strategy,
		pond.PanicHandler(func(p interface{}) {
			logger.Error("Worker pool panic recovered", "pool", cfg.Name, "panic", p)
		}),
	)

	return &WorkerPool{
		pool:   pool,
		config: cfg,
		logger: logger.WithField("component", "worker_pool").WithField("pool", cfg.Name),
	}
}

// Submit adds a task to the pool
func (wp *WorkerPool) Submit(task func()) error {
	if wp.config.NonBlocking {
		if !wp.pool.TrySubmit(task) {
			return fmt.Errorf("worker pool '%s' is full (capacity: %d)", wp.config.Name, wp.config.MaxCapacity)
		}
		return nil
	}

	// Blocking submit
	wp.pool.Submit(task)
	return nil
}

// SubmitAndWait submits a task and waits for it to complete
func (wp *WorkerPool) SubmitAndWait(task func()) {
	// Simple wait implementation without using TaskGroup for now
	done := make(chan struct{})
	wp.pool.Submit(func() {
		task()
		close(done)
	})
	<-done
}

// Stop stops the pool gracefully
func (wp *WorkerPool) Stop() {
	wp.pool.StopAndWait()
}

// Stats returns pool statistics
func (wp *WorkerPool) Stats() map[string]interface{} {
	return map[string]interface{}{
		"running_workers":  wp.pool.RunningWorkers(),
		"idle_workers":     wp.pool.IdleWorkers(),
		"submitted_tasks":  wp.pool.SubmittedTasks(),
		"waiting_tasks":    wp.pool.WaitingTasks(),
		"successful_tasks": wp.pool.SuccessfulTasks(),
		"failed_tasks":     wp.pool.FailedTasks(),
	}
}

// Resize allows dynamic resizing (wrapper for future use if needed)
// Pond supports dynamic resizing via logic but simplest is mostly fixed config.
