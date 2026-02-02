package order

import (
	"market_maker/pkg/logging"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOrderExecutor_BoundedErrorGrowth(t *testing.T) {
	// This test demonstrates the bounded behavior after the fix
	logger := logging.NewLogger(logging.InfoLevel, nil)
	oe := NewOrderExecutor(nil, logger)

	// Simulate 10,000 errors
	for i := 0; i < 10000; i++ {
		oe.recordError()
	}

	oe.errorMu.Lock()
	count := len(oe.errorTimestamps)
	oe.errorMu.Unlock()

	// Verify bounded growth (default 1000)
	assert.LessOrEqual(t, count, 1000, "Error tracking should be bounded")
}

func TestOrderExecutor_ConcurrentErrorRecording(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	oe := NewOrderExecutor(nil, logger)

	const numGoroutines = 10
	const errorsPerGoroutine = 1000

	done := make(chan bool)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < errorsPerGoroutine; j++ {
				oe.recordError()
			}
			done <- true
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	oe.errorMu.Lock()
	count := len(oe.errorTimestamps)
	oe.errorMu.Unlock()

	assert.LessOrEqual(t, count, 1000, "Error tracking should be bounded even under concurrency")
}

func TestOrderExecutor_SetErrorCapacity(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	oe := NewOrderExecutor(nil, logger)

	// Set capacity to 50
	oe.SetErrorCapacity(50)
	assert.Equal(t, 50, oe.errorCapacity)

	// Fill it
	for i := 0; i < 100; i++ {
		oe.recordError()
	}
	assert.Equal(t, 50, len(oe.errorTimestamps))

	// Decrease capacity
	oe.SetErrorCapacity(10)
	assert.Equal(t, 10, oe.errorCapacity)
	assert.Equal(t, 10, len(oe.errorTimestamps))
}
