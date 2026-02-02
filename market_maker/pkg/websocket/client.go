// Package websocket provides a reusable WebSocket client with automatic reconnection
package websocket

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/pkg/telemetry"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// MessageHandler handles incoming WebSocket messages
type MessageHandler func(message []byte)

// Client is a resilient WebSocket client
type Client struct {
	url           string
	handler       MessageHandler
	reconnectWait time.Duration

	conn *websocket.Conn
	mu   sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	onConnected func() // Callback when connected (useful for subscriptions)

	pingInterval time.Duration
	pingWait     time.Duration
	pongWait     time.Duration

	// Logger
	logger core.ILogger

	// OTel
	tracer      trace.Tracer
	msgCounter  metric.Int64Counter
	connCounter metric.Int64Counter
	latencyHist metric.Float64Histogram
}

// NewClient creates a new WebSocket client
func NewClient(url string, handler MessageHandler, logger core.ILogger) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	tracer := telemetry.GetTracer("ws-client")
	meter := telemetry.GetMeter("ws-client")

	msgCounter, _ := meter.Int64Counter("ws_messages_total",
		metric.WithDescription("Total number of WebSocket messages received"))
	connCounter, _ := meter.Int64Counter("ws_connections_total",
		metric.WithDescription("Total number of WebSocket connections initiated"))
	latencyHist, _ := meter.Float64Histogram("ws_message_processing_latency_seconds",
		metric.WithDescription("Latency of processing WebSocket messages in seconds"))

	return &Client{
		url:           url,
		handler:       handler,
		reconnectWait: 5 * time.Second,
		pingInterval:  30 * time.Second,
		pingWait:      10 * time.Second,
		pongWait:      60 * time.Second,
		ctx:           ctx,
		cancel:        cancel,
		tracer:        tracer,
		msgCounter:    msgCounter,
		connCounter:   connCounter,
		latencyHist:   latencyHist,
		logger:        logger,
	}
}

// SetPingConfig sets the ping/pong configuration
func (c *Client) SetPingConfig(interval, wait, pongWait time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pingInterval = interval
	c.pingWait = wait
	c.pongWait = pongWait
}

// SetOnConnected sets the callback for when the connection is established
func (c *Client) SetOnConnected(cb func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onConnected = cb
}

// Send sends a message over the WebSocket
func (c *Client) Send(message interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	return c.conn.WriteJSON(message)
}

// Start connects and begins listening for messages
func (c *Client) Start() {
	c.wg.Add(1)
	go c.runLoop()
}

// Stop closes the connection and stops the loop
func (c *Client) Stop() {
	c.cancel()

	// Wait for all goroutines to exit (with timeout)
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines exited cleanly
	case <-time.After(5 * time.Second):
		if c.logger != nil {
			c.logger.Warn("WebSocket client Stop: some goroutines did not exit within timeout")
		}
	}

	c.closeConn()
}

func (c *Client) runLoop() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			if err := c.connect(); err != nil {
				if c.logger != nil {
					c.logger.Error("WebSocket connect failed", "url", c.url, "error", err)
				}
				select {
				case <-c.ctx.Done():
					return
				case <-time.After(c.reconnectWait):
				}
				continue
			}

			c.mu.Lock()
			onConnected := c.onConnected
			pingInterval := c.pingInterval
			c.mu.Unlock()

			if onConnected != nil {
				onConnected()
			}

			// Start heartbeat if interval > 0
			heartbeatCtx, heartbeatCancel := context.WithCancel(c.ctx)
			if pingInterval > 0 {
				c.wg.Add(1)
				go c.heartbeat(heartbeatCtx, heartbeatCancel)
			}

			c.readLoop()
			heartbeatCancel()

			// If readLoop returns, connection was lost
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(c.reconnectWait):
			}
		}
	}
}

func (c *Client) heartbeat(ctx context.Context, cancel context.CancelFunc) {
	defer c.wg.Done()
	c.mu.Lock()
	interval := c.pingInterval
	wait := c.pingWait
	c.mu.Unlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()

			if conn == nil {
				return
			}

			if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(wait)); err != nil {
				// If ping fails, close connection to trigger reconnect
				c.closeConn()
				return
			}
		}
	}
}

func (c *Client) connect() error {
	ctx, span := c.tracer.Start(c.ctx, "WS Connect",
		trace.WithAttributes(attribute.String("ws.url", c.url)),
	)
	defer span.End()

	c.connCounter.Add(ctx, 1)

	c.mu.Lock()
	defer c.mu.Unlock()

	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Set pong handler
	conn.SetReadDeadline(time.Now().Add(c.pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(c.pongWait))
		return nil
	})

	c.conn = conn
	return nil
}

func (c *Client) closeConn() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) readLoop() {
	defer c.closeConn()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			if c.conn == nil {
				return
			}

			_, message, err := c.conn.ReadMessage()
			if err != nil {
				return
			}

			start := time.Now()
			c.msgCounter.Add(c.ctx, 1)

			if c.handler != nil {
				c.handler(message)
			}

			duration := time.Since(start).Seconds()
			c.latencyHist.Record(c.ctx, duration)
		}
	}
}
