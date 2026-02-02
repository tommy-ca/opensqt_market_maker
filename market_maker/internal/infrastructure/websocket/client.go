// Package websocket provides a reusable WebSocket client with automatic reconnection
package websocket

import (
	"context"
	"market_maker/internal/core"
	"sync"
	"time"

	"github.com/gorilla/websocket"
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

	logger core.ILogger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewClient creates a new WebSocket client
func NewClient(url string, handler MessageHandler, logger core.ILogger) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		url:           url,
		handler:       handler,
		reconnectWait: 5 * time.Second,
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start connects and begins listening for messages
func (c *Client) Start() {
	c.wg.Add(1)
	go c.runLoop()
}

// Stop closes the connection and stops the loop
func (c *Client) Stop() {
	c.cancel()
	c.wg.Wait()
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
					c.logger.Error("WebSocket connection failed", "error", err, "url", c.url)
				}
				time.Sleep(c.reconnectWait)
				continue
			}

			c.readLoop()

			// If readLoop returns, connection was lost
			time.Sleep(c.reconnectWait)
		}
	}
}

func (c *Client) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		return err
	}
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

			if c.handler != nil {
				c.handler(message)
			}
		}
	}
}
