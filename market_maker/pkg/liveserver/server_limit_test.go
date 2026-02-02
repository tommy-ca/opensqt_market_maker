package liveserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockLogger for testing
type MockLogger struct {
	mock.Mock
}

func (m *MockLogger) Debug(msg string, args ...interface{}) { m.Called(msg, args) }
func (m *MockLogger) Info(msg string, args ...interface{})  { m.Called(msg, args) }
func (m *MockLogger) Warn(msg string, args ...interface{})  { m.Called(msg, args) }
func (m *MockLogger) Error(msg string, args ...interface{}) { m.Called(msg, args) }
func (m *MockLogger) WithField(key string, value interface{}) interface{} {
	args := m.Called(key, value)
	return args.Get(0)
}

func TestServer_GlobalConnectionLimit(t *testing.T) {
	logger := new(MockLogger)
	logger.On("Warn", mock.Anything, mock.Anything).Return()
	logger.On("Info", mock.Anything, mock.Anything).Return()
	logger.On("Error", mock.Anything, mock.Anything).Return()

	hub := NewHub(logger)
	go hub.Run(context.Background())

	// Initialize server with limit = 2
	server := NewServer(hub, logger, []string{"*"})
	server.maxConnections = 2
	server.connSemaphore = make(chan struct{}, 2) // Manually injecting for test

	// Mock server
	s := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer s.Close()

	// Convert http URL to ws URL
	url := "ws" + strings.TrimPrefix(s.URL, "http")

	// Helper to dial with Origin header
	dial := func() (*websocket.Conn, *http.Response, error) {
		header := http.Header{}
		header.Set("Origin", "http://localhost") // Must match allowed origins or *
		// We encounter 101 Switching Protocols which is technically not an error for Dial,
		// but if server returns 429/503, Dial returns error "bad handshake" and non-nil response.
		return websocket.DefaultDialer.Dial(url, header)
	}

	// 1. First connection (OK)
	conn1, _, err := dial()
	assert.NoError(t, err)
	if conn1 != nil {
		defer conn1.Close()
	}

	// 2. Second connection (OK)
	conn2, _, err := dial()
	assert.NoError(t, err)
	if conn2 != nil {
		defer conn2.Close()
	}

	// 3. Third connection (Should Fail with 503)
	conn3, resp, err := dial()

	// We expect an error because handshake failed
	assert.Error(t, err)

	// Ensure conn is nil before closing (Fixes Panic)
	if conn3 != nil {
		conn3.Close()
	}

	if resp != nil {
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	} else {
		t.Error("Expected response with status code, got nil")
	}
}

func TestServer_IPRateLimit(t *testing.T) {
	logger := new(MockLogger)
	logger.On("Warn", mock.Anything, mock.Anything).Return()
	logger.On("Info", mock.Anything, mock.Anything).Return()

	hub := NewHub(logger)
	go hub.Run(context.Background())

	server := NewServer(hub, logger, []string{"*"})

	// Mock server
	s := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer s.Close()
	url := "ws" + strings.TrimPrefix(s.URL, "http")

	dial := func() (*websocket.Conn, *http.Response, error) {
		header := http.Header{}
		header.Set("Origin", "http://localhost")
		return websocket.DefaultDialer.Dial(url, header)
	}

	// Enable rate limit: low limit to trigger easily
	server.rateLimitEnabled = true // This was missing! NewServer sets it, but maybe test logic was confusing?
	// Ah, NewServer sets it to true by default.
	// But let's verify.

	server.rateLimit = 1.0
	server.rateBurst = 1

	// Re-init ipLimiters to clear defaults? No, NewServer initializes map but not limiters.
	// But getIPLimiter creates new limiter using current s.rateLimit.
	// So changing s.rateLimit now affects future IPs.

	// Ensure high global limit so we hit rate limit first
	server.maxConnections = 100
	server.connSemaphore = make(chan struct{}, 100)

	// 1. First connection (OK)
	conn1, _, err := dial()
	assert.NoError(t, err)
	if conn1 != nil {
		defer conn1.Close()
	}

	// 2. Second connection (Should Fail immediately due to burst=1)
	conn2, resp, err := dial()
	assert.Error(t, err)
	if conn2 != nil {
		conn2.Close()
	}

	if resp != nil {
		assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	}
}

func TestServer_ProductionWildcardOrigin(t *testing.T) {
	logger := new(MockLogger)
	logger.On("Warn", mock.Anything, mock.Anything).Return()
	logger.On("Info", mock.Anything, mock.Anything).Return()

	hub := NewHub(logger)
	go hub.Run(context.Background())

	// Server with wildcard and production = true
	server := NewServer(hub, logger, []string{"*"})
	server.SetProduction(true)

	// Mock server
	s := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer s.Close()
	url := "ws" + strings.TrimPrefix(s.URL, "http")

	dial := func() (*websocket.Conn, *http.Response, error) {
		header := http.Header{}
		header.Set("Origin", "http://evil.com")
		return websocket.DefaultDialer.Dial(url, header)
	}

	// Should fail because wildcard is rejected in production
	_, resp, err := dial()
	assert.Error(t, err)
	if resp != nil {
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	}
}
