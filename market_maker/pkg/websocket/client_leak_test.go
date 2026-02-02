package websocket

import (
	"market_maker/pkg/logging"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestGoroutineLeak(t *testing.T) {
	// Setup test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()

	// Convert http URL to ws URL
	url := "ws" + strings.TrimPrefix(server.URL, "http")

	// Get initial goroutine count
	// Give runtime a moment to settle
	time.Sleep(100 * time.Millisecond)
	initialGoroutines := runtime.NumGoroutine()

	logger := logging.NewLogger(logging.InfoLevel, nil)
	client := NewClient(url, func(message []byte) {}, logger)

	// Configure aggressive ping to ensure heartbeat starts
	client.SetPingConfig(10*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond)

	// Start client
	client.Start()

	// Wait for connection and heartbeat to spin up
	time.Sleep(200 * time.Millisecond)

	// Stop client
	client.Stop()

	// Give a moment for things to cleanup (if they are going to)
	// If Stop() works correctly, it should have already waited for heartbeat.
	// But we add small buffer for runtime scheduler.
	time.Sleep(50 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()

	// We expect final <= initial.
	// Allowing +1 variance for runtime internal things, but leaked heartbeat would definitely show up.
	// The leaky implementation has: runLoop (waited) + heartbeat (NOT waited).
	// If Stop() returns, runLoop is done. Heartbeat might still be running.
	// NOTE: This test is probabilistic. If heartbeat happens to finish fast, it passes.
	// But usually checking immediately after Stop() reveals the leak.

	assert.LessOrEqual(t, finalGoroutines, initialGoroutines+1, "Possible goroutine leak detected")
}
