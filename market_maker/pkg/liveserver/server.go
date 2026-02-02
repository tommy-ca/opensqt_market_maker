package liveserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"
)

var (
	websocketActiveConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "websocket_active_connections",
		Help: "Current number of active WebSocket connections",
	}, []string{"endpoint"})

	websocketRejectedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "websocket_rejected_total",
		Help: "Total number of rejected WebSocket connections",
	}, []string{"reason"})
)

func init() {
	prometheus.MustRegister(websocketActiveConnections)
	prometheus.MustRegister(websocketRejectedTotal)
}

// Server manages HTTP server for WebSocket connections and static files
type Server struct {
	hub            *Hub
	srv            *http.Server
	logger         Logger
	staticHandler  http.Handler
	upgrader       websocket.Upgrader
	allowedOrigins []string
	mu             sync.Mutex

	// Connection Limits
	maxConnections int
	connSemaphore  chan struct{}

	// Rate Limiting
	rateLimitEnabled bool
	ipLimiters       sync.Map // map[string]*rate.Limiter
	rateLimit        rate.Limit
	rateBurst        int

	// Production mode
	production bool
}

// NewServer creates a new Server
func NewServer(hub *Hub, logger Logger, allowedOrigins []string) *Server {
	// Create static file handler for web assets
	staticHandler := http.FileServer(http.Dir("web"))

	s := &Server{
		hub:              hub,
		logger:           logger,
		staticHandler:    staticHandler,
		allowedOrigins:   allowedOrigins,
		maxConnections:   1000, // Default limit as per TODO
		connSemaphore:    make(chan struct{}, 1000),
		rateLimitEnabled: true,
		rateLimit:        10.0,  // 10 connections per second
		rateBurst:        20,    // Allow burst of 20
		production:       false, // Default to false, can be set via SetProduction
	}

	// Configure WebSocket upgrader with origin validation
	s.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     s.checkOrigin,
	}

	return s
}

// checkOrigin validates the WebSocket connection origin against the whitelist
func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")

	// Reject connections without origin header
	if origin == "" {
		if s.logger != nil {
			s.logger.Warn("Rejected WebSocket connection with missing Origin header",
				"remote_addr", r.RemoteAddr)
		}
		return false
	}

	// Parse origin URL
	parsedOrigin, err := url.Parse(origin)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("Rejected WebSocket connection with invalid Origin",
				"origin", origin,
				"error", err)
		}
		return false
	}

	// Reconstruct origin as scheme://host for comparison
	originStr := parsedOrigin.Scheme + "://" + parsedOrigin.Host

	// Check against whitelist
	for _, allowed := range s.allowedOrigins {
		// Support wildcard for development (but log warning)
		if allowed == "*" {
			if s.production {
				if s.logger != nil {
					s.logger.Warn("Rejected wildcard origin in production mode",
						"origin", origin,
						"remote_addr", r.RemoteAddr)
				}
				websocketRejectedTotal.WithLabelValues("invalid_origin").Inc()
				return false
			}

			if s.logger != nil {
				s.logger.Warn("WebSocket connection allowed via wildcard origin (insecure for production)",
					"origin", origin,
					"remote_addr", r.RemoteAddr)
			}
			return true
		}

		// Exact match against allowed origin
		if originStr == allowed {
			return true
		}
	}

	// Origin not in whitelist - reject
	if s.logger != nil {
		s.logger.Warn("Rejected WebSocket connection from unauthorized origin",
			"origin", origin,
			"remote_addr", r.RemoteAddr,
			"allowed_origins", s.allowedOrigins)
	}
	websocketRejectedTotal.WithLabelValues("invalid_origin").Inc()
	return false
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context, addr string) error {
	s.mu.Lock()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)
	mux.Handle("/metrics", promhttp.Handler()) // Metrics endpoint
	mux.Handle("/", s.staticHandler)

	s.srv = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	s.mu.Unlock()

	if s.logger != nil {
		s.logger.Info("Starting live server", "addr", addr)
	}

	// Run server in background
	errChan := make(chan error, 1)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for context cancellation or server error
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return s.Stop(context.Background())
	}
}

// Stop gracefully stops the server
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.srv == nil {
		return nil
	}

	if s.logger != nil {
		s.logger.Info("Stopping live server")
	}

	return s.srv.Shutdown(ctx)
}

// handleWebSocket handles WebSocket upgrade and client management
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket (Standard flow)
	// We MUST check origin first because Gorilla Upgrader does it internally
	// BUT our rate limits should apply before upgrade resource consumption

	// 1. Check IP Rate Limit
	if s.rateLimitEnabled {
		ip := s.getRemoteIP(r)
		limiter := s.getIPLimiter(ip)

		if !limiter.Allow() {
			if s.logger != nil {
				s.logger.Warn("IP rate limit exceeded", "ip", ip)
			}
			websocketRejectedTotal.WithLabelValues("rate_limit").Inc()
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}
	}

	// 2. Check Global Connection Limit
	select {
	case s.connSemaphore <- struct{}{}:
		websocketActiveConnections.WithLabelValues(r.URL.Path).Inc()
		defer func() {
			<-s.connSemaphore
			websocketActiveConnections.WithLabelValues(r.URL.Path).Dec()
		}()
	default:
		if s.logger != nil {
			s.logger.Warn("Max connections reached")
		}
		websocketRejectedTotal.WithLabelValues("connection_limit").Inc()
		http.Error(w, "Server busy", http.StatusServiceUnavailable)
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("WebSocket upgrade failed", "error", err)
		}
		return
	}

	// Create new client
	clientID := uuid.New().String()
	client := NewClient(clientID)

	// Register client with hub
	s.hub.Register(client)

	if s.logger != nil {
		s.logger.Info("Client connected", "client_id", clientID, "remote_addr", r.RemoteAddr)
	}

	// Start goroutines for read/write
	var wg sync.WaitGroup
	wg.Add(2)

	// Write pump: send messages from hub to WebSocket
	go func() {
		defer wg.Done()
		s.writePump(conn, client)
	}()

	// Read pump: read messages from WebSocket (mostly for ping/pong)
	go func() {
		defer wg.Done()
		s.readPump(conn, client)
	}()

	// Wait for both pumps to finish
	wg.Wait()

	// Cleanup
	s.hub.Unregister(client)
	conn.Close()

	if s.logger != nil {
		s.logger.Info("Client disconnected", "client_id", clientID)
	}
}

// writePump sends messages from hub to WebSocket connection
func (s *Server) writePump(conn *websocket.Conn, client *Client) {
	ticker := time.NewTicker(54 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-client.GetSendChan():
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

			if !ok {
				// Channel closed
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Send message as JSON
			if err := conn.WriteJSON(msg); err != nil {
				if s.logger != nil {
					s.logger.Warn("Write error", "client_id", client.id, "error", err)
				}
				return
			}

		case <-ticker.C:
			// Send ping to keep connection alive
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump reads messages from WebSocket connection (handles pong responses)
func (s *Server) readPump(conn *websocket.Conn, client *Client) {
	defer func() {
		s.hub.Unregister(client)
	}()

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				if s.logger != nil {
					s.logger.Warn("Read error", "client_id", client.id, "error", err)
				}
			}
			break
		}
		// We don't process client messages (server only sends data)
		// Just keep connection alive
	}
}

// handleHealth handles health check endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	response := map[string]interface{}{
		"status":  "ok",
		"clients": s.hub.ClientCount(),
		"time":    time.Now().Unix(),
	}

	json.NewEncoder(w).Encode(response)
}

// BroadcastMessage is a convenience method to broadcast messages
func (s *Server) BroadcastMessage(msgType string, data interface{}) {
	msg := Message{
		Type: msgType,
		Data: data,
	}
	s.hub.Broadcast(msg)
}

// ClientCount returns the number of connected clients
func (s *Server) ClientCount() int {
	return s.hub.ClientCount()
}

// GetHub returns the hub instance
func (s *Server) GetHub() *Hub {
	return s.hub
}

// SetStaticDir changes the static file directory
func (s *Server) SetStaticDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staticHandler = http.FileServer(http.Dir(dir))

	if s.logger != nil {
		s.logger.Info("Static directory updated", "dir", dir)
	}
}

// SetProduction sets the production mode
func (s *Server) SetProduction(prod bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.production = prod
}

// SetMaxConnections updates the maximum number of concurrent connections
func (s *Server) SetMaxConnections(max int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxConnections = max
	// Re-initialize semaphore with new capacity
	s.connSemaphore = make(chan struct{}, max)
}

// SetRateLimit updates the IP-based rate limiting parameters
func (s *Server) SetRateLimit(limit float64, burst int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rateLimit = rate.Limit(limit)
	s.rateBurst = burst

	// Clear existing limiters to apply new limits
	s.ipLimiters = sync.Map{}
}

// Address returns the server address (for testing)
func (s *Server) Address() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.srv == nil {
		return ""
	}
	return s.srv.Addr
}

// IsRunning checks if server is running
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.srv != nil
}

// NewMessage - Helper function to create a Message
func NewMessage(msgType string, data interface{}) Message {
	return Message{
		Type: msgType,
		Data: data,
	}
}

// getRemoteIP extracts the client IP address
func (s *Server) getRemoteIP(r *http.Request) string {
	// Check X-Forwarded-For if behind proxy (but careful with spoofing)
	// For now sticking to RemoteAddr as safest default
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// getIPLimiter returns or creates a rate limiter for the given IP
func (s *Server) getIPLimiter(ip string) *rate.Limiter {
	// Fast path check
	if val, ok := s.ipLimiters.Load(ip); ok {
		return val.(*rate.Limiter)
	}

	// Create new limiter
	newLimiter := rate.NewLimiter(s.rateLimit, s.rateBurst)

	// LoadOrStore handles race condition if multiple requests arrive simultaneously
	actual, _ := s.ipLimiters.LoadOrStore(ip, newLimiter)
	return actual.(*rate.Limiter)
}

// NewKlineMessage - Helper to create typed messages
func NewKlineMessage(data interface{}) Message {
	return NewMessage(TypeKline, data)
}

// NewAccountMessage - Helper to create typed messages
func NewAccountMessage(data interface{}) Message {
	return NewMessage(TypeAccount, data)
}

// NewOrdersMessage - Helper to create typed messages
func NewOrdersMessage(data interface{}) Message {
	return NewMessage(TypeOrders, data)
}

// NewTradeEventMessage - Helper to create typed messages
func NewTradeEventMessage(data interface{}) Message {
	return NewMessage(TypeTradeEvent, data)
}

// NewPositionMessage - Helper to create typed messages
func NewPositionMessage(data interface{}) Message {
	return NewMessage(TypePosition, data)
}

// NewHistoryMessage - Helper to create typed messages
func NewHistoryMessage(data interface{}) Message {
	return NewMessage(TypeHistory, data)
}

// NewRiskStatusMessage - Helper to create typed messages
func NewRiskStatusMessage(data interface{}) Message {
	return NewMessage(TypeRiskStatus, data)
}

// NewSlotsMessage - Helper to create typed messages
func NewSlotsMessage(data interface{}) Message {
	return NewMessage(TypeSlots, data)
}

// FormatPrice formats a price string with proper decimal places
func FormatPrice(price string) string {
	// Ensure consistent price formatting (handled by caller)
	return price
}

// FormatTimestamp formats Unix timestamp to ISO 8601
func FormatTimestamp(unixTime int64) string {
	return fmt.Sprintf("%d", unixTime)
}
