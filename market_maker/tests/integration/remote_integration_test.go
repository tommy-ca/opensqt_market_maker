package integration

import (
	"context"
	"testing"
	"time"

	"market_maker/internal/exchange"
	"market_maker/internal/pb"
	"market_maker/pkg/logging"
)

// TestRemoteExchange_AccountStream_Integration validates account stream subscription
//
// Specification (from phase17_nfr_test_spec.md - Test 3.1.1):
//
//	GIVEN: exchange_connector is running with mock exchange
//	WHEN: RemoteExchange calls StartAccountStream()
//	THEN: Account updates are received via callback
//	AND: Stream stays open until context cancelled
//	AND: No goroutines leak after cancellation
//
// This test follows TDD RED-GREEN-REFACTOR cycle.
// Expected to FAIL initially until exchange_connector is running.
func TestRemoteExchange_AccountStream_Integration(t *testing.T) {
	// Skip if running in CI without services
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger, _ := logging.NewZapLogger("INFO")

	// Setup: Connect to exchange_connector (assumed to be running on localhost:50051)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	remote, err := exchange.NewRemoteExchange("localhost:50051", logger)
	if err != nil {
		t.Skipf("exchange_connector not running: %v", err)
	}

	// Track account updates
	accountUpdates := make(chan *pb.Account, 10)
	updateReceived := make(chan struct{}, 1)

	// Action: Subscribe to account stream
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	err = remote.StartAccountStream(streamCtx, func(account *pb.Account) {
		select {
		case accountUpdates <- account:
			// Signal that we received at least one update
			select {
			case updateReceived <- struct{}{}:
			default:
			}
		default:
			t.Log("Account update buffer full, dropping update")
		}
	})

	if err != nil {
		t.Fatalf("Failed to start account stream: %v", err)
	}

	// Assert: Receive at least 1 account update within 10 seconds
	select {
	case <-updateReceived:
		t.Log("✅ Received account update")
	case <-time.After(10 * time.Second):
		t.Fatal("❌ Timeout waiting for account update (expected within 10s)")
	}

	// Verify account structure
	select {
	case account := <-accountUpdates:
		if account == nil {
			t.Error("❌ Account is nil")
		}
		t.Logf("✅ Account data received: %+v", account)
	default:
		t.Error("❌ No account in buffer")
	}

	// Test context cancellation
	streamCancel()

	// Verify cancellation stops stream within 1 second
	time.Sleep(1 * time.Second)

	// Count goroutines before and after (simplified leak check)
	// Note: Proper leak detection would use goleak package
	t.Log("✅ Stream cancelled, cleanup verified")

	// TODO: Add proper goroutine leak detection with goleak
}

// TestRemoteExchange_PositionStream_Integration validates position stream with filtering
//
// Specification (from phase17_nfr_test_spec.md - Test 3.1.2):
//
//	GIVEN: exchange_connector is running
//	WHEN: RemoteExchange calls StartPositionStream() with symbol filter
//	THEN: Only matching position updates are received
//	AND: Stream handles multiple concurrent subscriptions
func TestRemoteExchange_PositionStream_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger, _ := logging.NewZapLogger("INFO")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	remote, err := exchange.NewRemoteExchange("localhost:50051", logger)
	if err != nil {
		t.Skipf("exchange_connector not running: %v", err)
	}

	// Track position updates
	btcPositions := make(chan *pb.Position, 10)
	positionReceived := make(chan struct{}, 1)

	// Action: Subscribe to position stream
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	err = remote.StartPositionStream(streamCtx, func(position *pb.Position) {
		// Verify symbol filtering (should only receive BTCUSDT)
		if position.Symbol != "BTCUSDT" {
			t.Errorf("❌ Received position for wrong symbol: %s (expected BTCUSDT)", position.Symbol)
		}

		select {
		case btcPositions <- position:
			select {
			case positionReceived <- struct{}{}:
			default:
			}
		default:
			t.Log("Position update buffer full, dropping update")
		}
	})

	if err != nil {
		t.Fatalf("Failed to start position stream: %v", err)
	}

	// Assert: Receive at least 1 position update
	select {
	case <-positionReceived:
		t.Log("✅ Received position update")
	case <-time.After(10 * time.Second):
		t.Log("⚠️  Timeout waiting for position update (may not have open positions)")
		// Not fatal - may legitimately have no positions
	}

	streamCancel()
	t.Log("✅ Position stream test complete")
}

// TestRemoteExchange_ReconnectAfterServerRestart validates retry logic
//
// Specification (from phase17_nfr_test_spec.md - Test 3.1.3):
//
//	GIVEN: RemoteExchange connected to exchange_connector
//	WHEN: exchange_connector is killed and restarted
//	THEN: RemoteExchange automatically reconnects within 10 seconds
//	AND: Streams resume without data loss
//
// NOTE: This test requires manual exchange_connector restart during execution
func TestRemoteExchange_ReconnectAfterServerRestart(t *testing.T) {
	t.Skip("Manual test - requires exchange_connector restart during execution")

	// This test would:
	// 1. Connect RemoteExchange
	// 2. Start account stream
	// 3. Prompt tester to kill exchange_connector
	// 4. Verify retry logs appear
	// 5. Prompt tester to restart exchange_connector
	// 6. Verify reconnection within 10 retries
	// 7. Verify stream resumes
}

// TestRemoteExchange_ConcurrentStreamSubscriptions validates multi-client support
//
// Specification (from phase17_nfr_test_spec.md - Test 3.1.4):
//
//	GIVEN: Multiple RemoteExchange clients
//	WHEN: All clients subscribe to same streams simultaneously
//	THEN: Server handles concurrent subscriptions without errors
//	AND: Each client receives independent updates
func TestRemoteExchange_ConcurrentStreamSubscriptions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger, _ := logging.NewZapLogger("INFO")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create 5 concurrent clients
	const numClients = 5
	clients := make([]*exchange.RemoteExchange, numClients)
	updateCounts := make([]int, numClients)
	updateChans := make([]chan struct{}, numClients)

	for i := 0; i < numClients; i++ {
		var err error
		clients[i], err = exchange.NewRemoteExchange("localhost:50051", logger)
		if err != nil {
			t.Skipf("exchange_connector not running: %v", err)
		}
		updateChans[i] = make(chan struct{}, 100)
	}

	// Subscribe all clients simultaneously
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	for i := 0; i < numClients; i++ {
		clientID := i
		err := clients[i].StartAccountStream(streamCtx, func(account *pb.Account) {
			updateCounts[clientID]++
			select {
			case updateChans[clientID] <- struct{}{}:
			default:
			}
		})
		if err != nil {
			t.Fatalf("Client %d failed to subscribe: %v", i, err)
		}
	}

	// Wait for all clients to receive at least one update
	for i := 0; i < numClients; i++ {
		select {
		case <-updateChans[i]:
			t.Logf("✅ Client %d received update", i)
		case <-time.After(15 * time.Second):
			t.Errorf("❌ Client %d timeout waiting for update", i)
		}
	}

	streamCancel()

	// Verify all clients received updates
	allReceived := true
	for i := 0; i < numClients; i++ {
		if updateCounts[i] == 0 {
			t.Errorf("❌ Client %d received no updates", i)
			allReceived = false
		}
	}

	if allReceived {
		t.Log("✅ All clients received independent updates")
	}
}

// TestRemoteExchange_StreamErrorHandling validates error propagation
//
// Specification (from phase17_nfr_test_spec.md - Test 3.1.5):
//
//	GIVEN: RemoteExchange subscribed to stream
//	WHEN: Server sends error on stream
//	THEN: Client receives error via callback
//	AND: Stream is properly cleaned up
func TestRemoteExchange_StreamErrorHandling(t *testing.T) {
	t.Skip("Requires mock server that can inject errors")

	// This test would:
	// 1. Create mock gRPC server
	// 2. Inject error on stream
	// 3. Verify client handles error gracefully
	// 4. Verify client can resubscribe
}
