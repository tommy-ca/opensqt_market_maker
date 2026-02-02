package websocket

import (
	"market_maker/pkg/logging"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketClient_Heartbeat(t *testing.T) {
	var pings int32
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		conn.SetPingHandler(func(string) error {
			atomic.AddInt32(&pings, 1)
			return conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(time.Second))
		})

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	logger, _ := logging.NewZapLogger("DEBUG")

	received := make(chan bool, 1)
	client := NewClient(url, func(message []byte) {
		received <- true
	}, logger)

	// Set very short ping interval for testing
	client.SetPingConfig(100*time.Millisecond, 50*time.Millisecond, 200*time.Millisecond)
	client.reconnectWait = 10 * time.Millisecond

	client.Start()
	defer client.Stop()

	// Wait for at least 2 pings
	time.Sleep(500 * time.Millisecond)

	if atomic.LoadInt32(&pings) < 2 {
		t.Errorf("Expected at least 2 pings, got %d", atomic.LoadInt32(&pings))
	}
}

func TestWebSocketClient_ReconnectOnTimeout(t *testing.T) {
	var connections int32
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&connections, 1)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Disable default ping handler to prevent automatic Pongs
		conn.SetPingHandler(func(string) error {
			return nil
		})

		// Do NOT handle pings to trigger timeout on client side
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	logger, _ := logging.NewZapLogger("DEBUG")

	client := NewClient(url, func(message []byte) {}, logger)

	// Short pong wait to trigger reconnect
	client.SetPingConfig(100*time.Millisecond, 50*time.Millisecond, 200*time.Millisecond)
	client.reconnectWait = 10 * time.Millisecond

	client.Start()
	defer client.Stop()

	// Wait for reconnects
	time.Sleep(600 * time.Millisecond)

	if atomic.LoadInt32(&connections) < 2 {
		t.Errorf("Expected multiple connections due to reconnects, got %d", atomic.LoadInt32(&connections))
	}
}
