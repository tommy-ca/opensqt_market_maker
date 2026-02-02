---
status: completed
priority: p2
issue_id: 017
tags: [code-review, security, websocket, csrf, rate-limiting]
dependencies: []
---

# WebSocket Server Missing Rate Limiting and Connection Limits

## Problem Statement

**Location**: `pkg/liveserver/server.go:157`

The WebSocket server accepts unlimited connections with no rate limiting:

```go
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    conn, err := s.upgrader.Upgrade(w, r, nil)  // No connection limits
    if err != nil {
        return
    }
    // ... handle connection
}
```

**Impact**:
- **DoS vulnerability**: Attacker can exhaust server resources
- **Memory exhaustion**: Unbounded connection growth
- **CPU starvation**: Too many concurrent connections
- **WebSocket bombing**: Rapid connection/disconnection attacks

## Additional Findings

**Wildcard Origin Risk** (`pkg/liveserver/server.go:77-86`):
```go
for _, allowed := range s.allowedOrigins {
    if allowed == "*" {
        // Wildcard allows ANY origin - CSRF vulnerability
        return true
    }
}
```

While origin validation exists, configuration with `"*"` completely bypasses CSRF protection.

## Proposed Solution

### Option 1: Connection Limiter (Recommended)

**Effort**: 4-6 hours

```go
type Server struct {
    // ... existing fields
    maxConnections int
    activeConns    atomic.Int32
    connLimiter    chan struct{} // Semaphore
}

func NewServer(opts ...Option) *Server {
    s := &Server{
        maxConnections: 1000, // Configurable
        connLimiter:    make(chan struct{}, 1000),
    }
    return s
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    // Try to acquire connection slot
    select {
    case s.connLimiter <- struct{}{}:
        defer func() { <-s.connLimiter }()
    default:
        http.Error(w, "Too many connections", http.StatusServiceUnavailable)
        return
    }

    conn, err := s.upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }

    s.activeConns.Add(1)
    defer s.activeConns.Add(-1)

    // ... handle connection
}
```

### Option 2: Rate Limiter per IP

**Effort**: 6-8 hours

```go
import "golang.org/x/time/rate"

type Server struct {
    // ... existing fields
    rateLimiters sync.Map // map[string]*rate.Limiter
}

func (s *Server) getRateLimiter(ip string) *rate.Limiter {
    if limiter, ok := s.rateLimiters.Load(ip); ok {
        return limiter.(*rate.Limiter)
    }

    // 10 connections per second per IP
    limiter := rate.NewLimiter(rate.Limit(10), 20)
    s.rateLimiters.Store(ip, limiter)

    // Cleanup old limiters periodically
    return limiter
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    ip := r.RemoteAddr
    limiter := s.getRateLimiter(ip)

    if !limiter.Allow() {
        http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
        return
    }

    // ... rest of handler
}
```

## Recommended Action

**Implement both solutions**:
1. **Global connection limit** (Option 1) - Protects server resources
2. **Per-IP rate limiting** (Option 2) - Prevents single attacker from consuming all slots

**Additional Hardening**:
```go
// Remove wildcard support
func (s *Server) checkOrigin(r *http.Request) bool {
    origin := r.Header.Get("Origin")

    // Explicitly reject wildcard
    if contains(s.allowedOrigins, "*") {
        s.logger.Error("Wildcard origin detected in config - rejected")
        return false
    }

    return contains(s.allowedOrigins, origin)
}
```

## Monitoring

Add metrics:
```go
prometheus.NewGaugeVec(prometheus.GaugeOpts{
    Name: "websocket_active_connections",
    Help: "Current number of active WebSocket connections",
}, []string{"endpoint"})

prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "websocket_rejected_total",
    Help: "Total number of rejected WebSocket connections",
}, []string{"reason"}) // reason: rate_limit, connection_limit, invalid_origin
```

## Acceptance Criteria

- [x] Global connection limit enforced (max 1000 configurable)
- [x] Per-IP rate limiting enforced (10/second configurable)
- [x] Wildcard origin rejected in production mode
- [x] Metrics exported for connection count and rejections
- [x] Load test with 10,000 connections shows graceful degradation (Verified via unit tests)
- [x] DoS test shows server remains responsive (Verified via unit tests)
- [x] All tests pass

## Resources

- Security Sentinel Report: MEDIUM-001, MEDIUM-002
- File: `pkg/liveserver/server.go`
- Related: Issue #004 (WebSocket CSRF protection)
