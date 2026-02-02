package liveserver

import (
	"context"
	"encoding/json"
	"market_maker/pkg/logging"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewServer verifies server creation
func TestNewServer(t *testing.T) {
	hub := NewHub(nil)
	allowedOrigins := []string{"http://localhost:8081"}
	server := NewServer(hub, nil, allowedOrigins)

	assert.NotNil(t, server)
	assert.Equal(t, hub, server.hub)
	assert.Equal(t, allowedOrigins, server.allowedOrigins)
}

// TestServerWebSocketUpgrade verifies WebSocket upgrade
func TestServerWebSocketUpgrade(t *testing.T) {
	hub := NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	// Allow all origins for this test
	logger := logging.NewLogger(logging.DebugLevel, nil)
	server := NewServer(hub, logger, []string{"*"})

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create dialer with Origin header
	dialer := websocket.Dialer{}
	headers := http.Header{}
	headers.Set("Origin", "http://test.local")

	// Connect WebSocket client
	ws, _, err := dialer.Dial(wsURL, headers)
	require.NoError(t, err)
	defer ws.Close()

	// Wait for client registration
	time.Sleep(50 * time.Millisecond)

	// Hub should have 1 client
	assert.Equal(t, 1, hub.ClientCount())

	// Close WebSocket
	ws.Close()
	time.Sleep(50 * time.Millisecond)

	// Hub should have 0 clients after disconnect
	assert.Equal(t, 0, hub.ClientCount())
}

// TestServerReceiveMessage verifies client receives broadcast messages
func TestServerReceiveMessage(t *testing.T) {
	hub := NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	server := NewServer(hub, nil, []string{"*"})

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create dialer with Origin header
	dialer := websocket.Dialer{}
	headers := http.Header{}
	headers.Set("Origin", "http://test.local")

	// Connect WebSocket client
	ws, _, err := dialer.Dial(wsURL, headers)
	require.NoError(t, err)
	defer ws.Close()

	// Wait for registration
	time.Sleep(50 * time.Millisecond)

	// Broadcast a message
	msg := Message{
		Type: TypeKline,
		Data: map[string]interface{}{
			"time":  1705881600,
			"price": "42000.00",
		},
	}
	hub.Broadcast(msg)

	// Client should receive the message
	var received Message
	err = ws.ReadJSON(&received)
	require.NoError(t, err)

	assert.Equal(t, msg.Type, received.Type)

	// Verify data structure
	data, ok := received.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "42000.00", data["price"])
}

// TestServerMultipleClients verifies multiple WebSocket clients
func TestServerMultipleClients(t *testing.T) {
	hub := NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	server := NewServer(hub, nil, []string{"*"})

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create dialer with Origin header
	dialer := websocket.Dialer{}
	headers := http.Header{}
	headers.Set("Origin", "http://test.local")

	// Connect 3 WebSocket clients
	clients := make([]*websocket.Conn, 3)
	for i := 0; i < 3; i++ {
		ws, _, err := dialer.Dial(wsURL, headers)
		require.NoError(t, err)
		defer ws.Close()
		clients[i] = ws
	}

	// Wait for registrations
	time.Sleep(50 * time.Millisecond)

	// Hub should have 3 clients
	assert.Equal(t, 3, hub.ClientCount())

	// Broadcast a message
	msg := Message{
		Type: TypeAccount,
		Data: map[string]interface{}{
			"balance": "10000.00",
		},
	}
	hub.Broadcast(msg)

	// All clients should receive the message
	for i, ws := range clients {
		var received Message
		err := ws.ReadJSON(&received)
		require.NoError(t, err, "Client %d should receive message", i)
		assert.Equal(t, msg.Type, received.Type)
	}
}

// TestServerPingPong verifies WebSocket ping/pong handling
func TestServerPingPong(t *testing.T) {
	hub := NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	server := NewServer(hub, nil, []string{"*"})

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create dialer with Origin header
	dialer := websocket.Dialer{}
	headers := http.Header{}
	headers.Set("Origin", "http://test.local")

	// Connect WebSocket client
	ws, _, err := dialer.Dial(wsURL, headers)
	require.NoError(t, err)
	defer ws.Close()

	// Wait for registration
	time.Sleep(50 * time.Millisecond)

	// Verify connection is alive by sending a test message
	// The server's readPump will handle it
	msg := Message{Type: TypeKline, Data: "test"}
	hub.Broadcast(msg)

	// Should receive the broadcast message
	ws.SetReadDeadline(time.Now().Add(1 * time.Second))
	var received Message
	err = ws.ReadJSON(&received)
	require.NoError(t, err)
	assert.Equal(t, TypeKline, received.Type)
}

// TestServerHealthEndpoint verifies health check endpoint
func TestServerHealthEndpoint(t *testing.T) {
	hub := NewHub(nil)
	server := NewServer(hub, nil, []string{"*"})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "ok", response["status"])
	assert.NotNil(t, response["clients"])
}

// TestServerStaticFiles verifies static file serving
func TestServerStaticFiles(t *testing.T) {
	hub := NewHub(nil)
	server := NewServer(hub, nil, []string{"*"})

	// Test that static handler is created
	assert.NotNil(t, server.staticHandler)
}

// TestServerStart verifies server start and stop
func TestServerStart(t *testing.T) {
	hub := NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	server := NewServer(hub, nil, []string{"*"})

	// Start server on random port
	go func() {
		err := server.Start(ctx, ":0")
		assert.NoError(t, err)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Stop server
	err := server.Stop(context.Background())
	assert.NoError(t, err)
}

// TestServerConcurrentConnections verifies handling many concurrent connections
func TestServerConcurrentConnections(t *testing.T) {
	hub := NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	server := NewServer(hub, nil, []string{"*"})

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create dialer with Origin header
	dialer := websocket.Dialer{}
	headers := http.Header{}
	headers.Set("Origin", "http://test.local")

	// Connect 10 clients concurrently
	clientCount := 10
	clients := make([]*websocket.Conn, clientCount)

	for i := 0; i < clientCount; i++ {
		ws, _, err := dialer.Dial(wsURL, headers)
		require.NoError(t, err)
		defer ws.Close()
		clients[i] = ws
	}

	// Wait for all registrations
	time.Sleep(100 * time.Millisecond)

	// Hub should have all clients
	assert.Equal(t, clientCount, hub.ClientCount())

	// Broadcast a message
	msg := Message{
		Type: TypePosition,
		Data: map[string]interface{}{
			"symbol":   "BTCUSDT",
			"size":     "1.5",
			"avgPrice": "42000.00",
		},
	}
	hub.Broadcast(msg)

	// All clients should receive
	for i, ws := range clients {
		var received Message
		err := ws.ReadJSON(&received)
		require.NoError(t, err, "Client %d should receive message", i)
		assert.Equal(t, msg.Type, received.Type)
	}
}

// TestOriginValidation_AllowedOrigin verifies that connections from allowed origins are accepted
func TestOriginValidation_AllowedOrigin(t *testing.T) {
	hub := NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	// Configure server with specific allowed origins
	allowedOrigins := []string{"http://localhost:3000", "http://localhost:8081"}
	server := NewServer(hub, nil, allowedOrigins)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create dialer with allowed origin
	dialer := websocket.Dialer{}
	headers := http.Header{}
	headers.Set("Origin", "http://localhost:3000")

	// Connect WebSocket client with allowed origin
	ws, resp, err := dialer.Dial(wsURL, headers)
	require.NoError(t, err)
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	defer ws.Close()

	// Wait for registration
	time.Sleep(50 * time.Millisecond)

	// Hub should have 1 client
	assert.Equal(t, 1, hub.ClientCount())
}

// TestOriginValidation_UnauthorizedOrigin verifies that connections from unauthorized origins are rejected
func TestOriginValidation_UnauthorizedOrigin(t *testing.T) {
	hub := NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	// Configure server with specific allowed origins (not including the test origin)
	allowedOrigins := []string{"http://localhost:3000", "http://localhost:8081"}
	server := NewServer(hub, nil, allowedOrigins)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create dialer with unauthorized origin
	dialer := websocket.Dialer{}
	headers := http.Header{}
	headers.Set("Origin", "http://evil.com")

	// Attempt to connect WebSocket client with unauthorized origin
	ws, resp, err := dialer.Dial(wsURL, headers)

	// Connection should be rejected
	assert.Error(t, err)
	if resp != nil {
		assert.NotEqual(t, http.StatusSwitchingProtocols, resp.StatusCode)
	}
	if ws != nil {
		ws.Close()
	}

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Hub should have 0 clients (connection rejected)
	assert.Equal(t, 0, hub.ClientCount())
}

// TestOriginValidation_MissingOrigin verifies that connections without Origin header are rejected
func TestOriginValidation_MissingOrigin(t *testing.T) {
	hub := NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	// Configure server with specific allowed origins
	allowedOrigins := []string{"http://localhost:3000"}
	server := NewServer(hub, nil, allowedOrigins)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create dialer without Origin header (empty headers)
	dialer := websocket.Dialer{}
	headers := http.Header{}
	// Explicitly do not set Origin header

	// Attempt to connect WebSocket client without origin
	ws, resp, err := dialer.Dial(wsURL, headers)

	// Connection should be rejected
	assert.Error(t, err)
	if resp != nil {
		assert.NotEqual(t, http.StatusSwitchingProtocols, resp.StatusCode)
	}
	if ws != nil {
		ws.Close()
	}

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Hub should have 0 clients (connection rejected)
	assert.Equal(t, 0, hub.ClientCount())
}

// TestOriginValidation_WildcardOrigin verifies that wildcard allows all origins (with warning)
func TestOriginValidation_WildcardOrigin(t *testing.T) {
	hub := NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	// Configure server with wildcard origin
	allowedOrigins := []string{"*"}
	server := NewServer(hub, nil, allowedOrigins)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Test with any origin - should be accepted
	dialer := websocket.Dialer{}
	headers := http.Header{}
	headers.Set("Origin", "http://any-random-domain.com")

	ws, resp, err := dialer.Dial(wsURL, headers)
	require.NoError(t, err)
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	defer ws.Close()

	// Wait for registration
	time.Sleep(50 * time.Millisecond)

	// Hub should have 1 client (wildcard accepted)
	assert.Equal(t, 1, hub.ClientCount())
}

// TestOriginValidation_MultipleAllowedOrigins verifies multiple origins in whitelist
func TestOriginValidation_MultipleAllowedOrigins(t *testing.T) {
	hub := NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	// Configure server with multiple allowed origins
	allowedOrigins := []string{
		"http://localhost:3000",
		"http://localhost:8081",
		"https://app.example.com",
		"http://127.0.0.1:3000",
	}
	server := NewServer(hub, nil, allowedOrigins)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Test each allowed origin
	testOrigins := []string{
		"http://localhost:3000",
		"http://localhost:8081",
		"https://app.example.com",
		"http://127.0.0.1:3000",
	}

	for _, origin := range testOrigins {
		dialer := websocket.Dialer{}
		headers := http.Header{}
		headers.Set("Origin", origin)

		ws, resp, err := dialer.Dial(wsURL, headers)
		require.NoError(t, err, "Origin %s should be allowed", origin)
		assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
		ws.Close()

		// Wait for cleanup
		time.Sleep(50 * time.Millisecond)
	}
}
