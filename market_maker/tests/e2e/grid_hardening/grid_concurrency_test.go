package gridhardening

import (
	"context"
	"testing"
	"time"

	"market_maker/internal/pb"
	"market_maker/pkg/pbu"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BlockingExchange wraps SimulatedExchange to support Signal-and-Wait testing
type BlockingExchange struct {
	*SimulatedExecutor // Embed to delegate normal execution
	Ready              chan struct{}
	Finish             chan struct{}
}

// Override Execute (Wait, we can't override methods of embedded struct easily if we use SimulatedExecutor)
// Better to make BlockingExchange implement core.IExchange? No, the coordinator uses Executor.
// We need a BlockingExecutor.

type BlockingExecutor struct {
	*SimulatedExecutor
	Ready  chan struct{}
	Finish chan struct{}
}

func (e *BlockingExecutor) Execute(ctx context.Context, actions []*pb.OrderAction) {
	// Signal we are here
	select {
	case e.Ready <- struct{}{}:
	default:
	}

	// Wait for finish signal
	if e.Finish != nil {
		<-e.Finish
	}

	// Delegate
	e.SimulatedExecutor.Execute(ctx, actions)
}

func TestGridHardening_Concurrency_BlockingIO(t *testing.T) {
	t.Parallel()

	// 1. Setup
	_, exch, _, _ := SetupGridEngine(t)
	// We need to inject our BlockingExecutor.
	// SetupGridEngine returns initialized Coordinator. We can't easily swap Executor inside it?
	// GridCoordinator struct has unexported `executor`.
	// But we can create a new Coordinator manually or modify SetupGridEngine.
	// Let's modify SetupGridEngine to accept options? Or just manually construct for this test.

	// Manual construction to inject BlockingExecutor
	ready := make(chan struct{})
	finish := make(chan struct{})

	// Reuse Setup parts
	baseExecutor := &SimulatedExecutor{exchange: exch}
	blockingExecutor := &BlockingExecutor{
		SimulatedExecutor: baseExecutor,
		Ready:             ready,
		Finish:            finish,
	}

	// Re-construct coordinator with blocking executor
	// We need access to deps.
	// Since we can't easily access deps used in SetupGridEngine, let's just make a new Setup function or copy-paste setup logic.
	// Copy-pasting logic for isolation:

	store, _ := SetupStore()
	logger := SetupLogger()
	meter := SetupMeter()

	// Re-create PM
	pm := SetupPM(store, logger, meter)

	// Deps
	deps := SetupDeps(exch, pm, store, logger, blockingExecutor) // Inject BlockingExecutor
	coord := SetupCoordinator(deps)

	// Start (no engine wrapper needed, we test Coordinator directly)
	ctx := context.Background()
	require.NoError(t, coord.Start(ctx))

	// 2. Trigger Slow Operation (Async)
	// We need an action that triggers Execute.
	// Price update outside range triggers rebalance/execution.

	price := &pb.PriceChange{
		Symbol: "BTCUSDT",
		Price:  pbu.FromGoDecimal(decimal.NewFromInt(50000)),
	}

	go func() {
		// This should call Execute -> Block
		_ = coord.OnPriceUpdate(ctx, price)
	}()

	// 3. Wait for Ready signal (Coordinator is now blocked in Execute)
	select {
	case <-ready:
		// Coordinator has acquired lock, calculated actions, and is now "executing" (blocked)
		// Crucially, it should have RELEASED the lock before calling Execute.
	case <-time.After(1 * time.Second):
		t.Fatal("Timed out waiting for Coordinator to call Execute")
	}

	// 4. Trigger Fast Operation (Sync)
	// If lock is held, this will block until Finish is signaled.
	// If lock is released, this will proceed immediately.

	start := time.Now()
	// Send another price update (or any method requiring lock)
	price2 := &pb.PriceChange{
		Symbol: "BTCUSDT",
		Price:  pbu.FromGoDecimal(decimal.NewFromInt(50001)),
	}
	err := coord.OnPriceUpdate(ctx, price2)
	elapsed := time.Since(start)

	assert.NoError(t, err)
	assert.Less(t, elapsed, 10*time.Millisecond, "OnPriceUpdate blocked by slow execution! Lock not released?")

	// 5. Cleanup
	close(finish) // Let the slow op finish
}
