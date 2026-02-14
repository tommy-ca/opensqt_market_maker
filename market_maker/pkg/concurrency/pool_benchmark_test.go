package concurrency

import (
	"market_maker/internal/core"
	"sync"
	"sync/atomic"
	"testing"
)

type noopLogger struct{}

func (l *noopLogger) Debug(msg string, fields ...interface{})               {}
func (l *noopLogger) Info(msg string, fields ...interface{})                {}
func (l *noopLogger) Warn(msg string, fields ...interface{})                {}
func (l *noopLogger) Error(msg string, fields ...interface{})               {}
func (l *noopLogger) Fatal(msg string, fields ...interface{})               {}
func (l *noopLogger) WithField(key string, value interface{}) core.ILogger  { return l }
func (l *noopLogger) WithFields(fields map[string]interface{}) core.ILogger { return l }

func BenchmarkWorkerPool_Submit(b *testing.B) {
	pool := NewWorkerPool(PoolConfig{
		Name:        "BenchmarkPool",
		MaxWorkers:  10,
		MaxCapacity: 1000,
		NonBlocking: false,
	}, &noopLogger{})
	defer pool.Stop()

	b.ResetTimer()
	var counter int64
	for i := 0; i < b.N; i++ {
		_ = pool.Submit(func() {
			atomic.AddInt64(&counter, 1)
		})
	}
}

func BenchmarkWorkerPool_SubmitAndWait(b *testing.B) {
	pool := NewWorkerPool(PoolConfig{
		Name:        "BenchmarkPoolWait",
		MaxWorkers:  10,
		MaxCapacity: 1000,
		NonBlocking: false,
	}, &noopLogger{})
	defer pool.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.SubmitAndWait(func() {
			// work
		})
	}
}

func BenchmarkGoroutine_Spawn(b *testing.B) {
	var wg sync.WaitGroup
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			wg.Done()
		}()
	}
	wg.Wait()
}
