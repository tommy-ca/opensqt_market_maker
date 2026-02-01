package integration

import (
	"context"
	"testing"
	"time"

	"market_maker/internal/exchange"
	"market_maker/internal/mock"
	"market_maker/pkg/logging"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTLSEncryption verifies that gRPC communications use TLS encryption
func TestTLSEncryption(t *testing.T) {
	logger, _ := logging.NewZapLogger("INFO")

	// 1. Start mock exchange server with TLS
	mockExch := mock.NewMockExchange("mock-binance")
	server := exchange.NewExchangeServer(mockExch, logger)

	port := 50052
	certFile := "../../certs/server-cert.pem"
	keyFile := "../../certs/server-key.pem"

	// Start server with TLS in background
	errCh := make(chan error, 1)
	go func() {
		err := server.StartWithTLS(port, certFile, keyFile)
		if err != nil {
			errCh <- err
		}
	}()

	// Wait for server to start
	time.Sleep(500 * time.Millisecond)

	// 2. Connect client with TLS
	client, err := exchange.NewRemoteExchangeWithTLS(
		"localhost:50052",
		logger,
		certFile,
		"localhost",
	)
	require.NoError(t, err, "Failed to create TLS client")

	// 3. Verify connection works
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	name := client.GetName()
	assert.Equal(t, "mock-binance", name, "Should get correct exchange name over TLS")

	// 4. Test actual operations over TLS
	err = client.CheckHealth(ctx)
	assert.NoError(t, err, "Health check should succeed over TLS")

	// Test data transmission (orders contain sensitive info)
	positions, err := client.GetPositions(ctx, "BTCUSDT")
	assert.NoError(t, err, "Should be able to fetch positions over TLS")
	// Positions can be empty slice, just verify no error occurred
	_ = positions

	t.Log("✅ TLS encryption test passed - gRPC traffic is encrypted")
}

// TestTLSServerOnly verifies that the server can run with TLS
func TestTLSServerOnly(t *testing.T) {
	logger, _ := logging.NewZapLogger("INFO")

	mockExch := mock.NewMockExchange("mock-test")
	server := exchange.NewExchangeServer(mockExch, logger)

	certFile := "../../certs/server-cert.pem"
	keyFile := "../../certs/server-key.pem"

	errCh := make(chan error, 1)
	go func() {
		err := server.StartWithTLS(50053, certFile, keyFile)
		if err != nil {
			errCh <- err
		}
	}()

	// Wait for server to start
	time.Sleep(500 * time.Millisecond)

	// Verify no immediate errors
	select {
	case err := <-errCh:
		t.Fatalf("Server failed to start with TLS: %v", err)
	default:
		t.Log("✅ Server started successfully with TLS encryption")
	}
}

// TestInsecureConnectionFails verifies that connecting without TLS to a TLS server fails
func TestInsecureToTLSServerFails(t *testing.T) {
	logger, _ := logging.NewZapLogger("INFO")

	// Start TLS server
	mockExch := mock.NewMockExchange("mock-secure")
	server := exchange.NewExchangeServer(mockExch, logger)

	certFile := "../../certs/server-cert.pem"
	keyFile := "../../certs/server-key.pem"

	go func() {
		_ = server.StartWithTLS(50054, certFile, keyFile)
	}()

	time.Sleep(500 * time.Millisecond)

	// Try to connect without TLS (should fail or have issues)
	client, err := exchange.NewRemoteExchange("localhost:50054", logger)

	// The connection might be created, but operations should fail
	if err == nil && client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// This should fail because we're trying insecure connection to TLS server
		err = client.CheckHealth(ctx)
		assert.Error(t, err, "Insecure connection to TLS server should fail")
		t.Log("✅ Correctly rejected insecure connection to TLS server")
	} else {
		t.Log("✅ Failed to create insecure connection to TLS server (expected)")
	}
}

// TestTLSWithRealOperations tests realistic trading operations over TLS
func TestTLSWithRealOperations(t *testing.T) {
	logger, _ := logging.NewZapLogger("INFO")

	// Setup server
	mockExch := mock.NewMockExchange("mock-trading")
	server := exchange.NewExchangeServer(mockExch, logger)

	certFile := "../../certs/server-cert.pem"
	keyFile := "../../certs/server-key.pem"

	go func() {
		_ = server.StartWithTLS(50055, certFile, keyFile)
	}()

	time.Sleep(500 * time.Millisecond)

	// Setup client
	client, err := exchange.NewRemoteExchangeWithTLS(
		"localhost:50055",
		logger,
		certFile,
		"localhost",
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test multiple operations that would transmit sensitive data
	t.Run("GetAccount", func(t *testing.T) {
		account, err := client.GetAccount(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, account)
		t.Log("✅ Account data transmitted securely over TLS")
	})

	t.Run("GetPositions", func(t *testing.T) {
		positions, err := client.GetPositions(ctx, "BTCUSDT")
		assert.NoError(t, err)
		// Positions can be empty, just verify no error
		_ = positions
		t.Log("✅ Position data transmitted securely over TLS")
	})

	t.Run("GetOpenOrders", func(t *testing.T) {
		orders, err := client.GetOpenOrders(ctx, "BTCUSDT", false)
		assert.NoError(t, err)
		// Orders can be empty, just verify no error
		_ = orders
		t.Log("✅ Order data transmitted securely over TLS")
	})

	t.Run("GetLatestPrice", func(t *testing.T) {
		price, err := client.GetLatestPrice(ctx, "BTCUSDT")
		assert.NoError(t, err)
		assert.True(t, price.GreaterThan(decimal.Zero))
		t.Log("✅ Price data transmitted securely over TLS")
	})
}

// BenchmarkTLSOverhead measures the performance impact of TLS encryption
func BenchmarkTLSOverhead(b *testing.B) {
	logger, _ := logging.NewZapLogger("ERROR") // Quiet logs for benchmark

	// Setup TLS server
	mockExch := mock.NewMockExchange("benchmark")
	server := exchange.NewExchangeServer(mockExch, logger)

	certFile := "../../certs/server-cert.pem"
	keyFile := "../../certs/server-key.pem"

	go func() {
		_ = server.StartWithTLS(50056, certFile, keyFile)
	}()

	time.Sleep(500 * time.Millisecond)

	client, err := exchange.NewRemoteExchangeWithTLS(
		"localhost:50056",
		logger,
		certFile,
		"localhost",
	)
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.CheckHealth(ctx)
	}
}
