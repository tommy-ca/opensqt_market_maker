package liveserver

import (
	"context"
	"sync"
)

// Client represents a WebSocket client connection
type Client struct {
	id     string
	send   chan Message
	mu     sync.Mutex
	closed bool
}

// NewClient creates a new client
func NewClient(id string) *Client {
	return &Client{
		id:   id,
		send: make(chan Message, 256), // Buffered to prevent blocking
	}
}

// Send sends a message to the client (non-blocking)
func (c *Client) Send(msg Message) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return false
	}

	select {
	case c.send <- msg:
		return true
	default:
		// Channel full, client is slow
		return false
	}
}

// GetSendChan returns the send channel for reading
func (c *Client) GetSendChan() <-chan Message {
	return c.send
}

// Close closes the client
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.closed {
		c.closed = true
		close(c.send)
	}
}

// Hub manages WebSocket client connections and broadcasts messages
type Hub struct {
	// Registered clients
	clients map[*Client]bool

	// Broadcast message to all clients
	broadcast chan Message

	// Register client
	register chan *Client

	// Unregister client
	unregister chan *Client

	// Mutex for client map
	mu sync.RWMutex

	// Logger (optional)
	logger Logger

	// Context for shutdown
	ctx context.Context
}

// Logger is a simple logging interface
type Logger interface {
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
}

// NewHub creates a new Hub
func NewHub(logger Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan Message, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		logger:     logger,
	}
}

// Run starts the hub's main loop
func (h *Hub) Run(ctx context.Context) {
	h.ctx = ctx

	for {
		select {
		case <-ctx.Done():
			// Shutdown: close all clients
			h.mu.Lock()
			for client := range h.clients {
				client.Close()
				delete(h.clients, client)
			}
			h.mu.Unlock()
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			if h.logger != nil {
				h.logger.Info("Client registered", "client_id", client.id, "total_clients", len(h.clients))
			}

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.Close()
			}
			h.mu.Unlock()
			if h.logger != nil {
				h.logger.Info("Client unregistered", "client_id", client.id, "total_clients", len(h.clients))
			}

		case message := <-h.broadcast:
			h.mu.RLock()
			// Copy clients to avoid holding lock during broadcast
			clientList := make([]*Client, 0, len(h.clients))
			for client := range h.clients {
				clientList = append(clientList, client)
			}
			h.mu.RUnlock()

			// Broadcast to all clients (outside lock)
			for _, client := range clientList {
				success := client.Send(message)
				if !success {
					// Client is slow or disconnected, unregister
					select {
					case h.unregister <- client:
					default:
					}
				}
			}
		}
	}
}

// Register registers a client
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Unregister unregisters a client
func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

// Broadcast broadcasts a message to all clients
func (h *Hub) Broadcast(msg Message) {
	select {
	case h.broadcast <- msg:
		// Success
	default:
		// Broadcast channel full, log warning
		if h.logger != nil {
			h.logger.Warn("Broadcast channel full, dropping message", "type", msg.Type)
		}
	}
}

// ClientCount returns the current number of connected clients
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
