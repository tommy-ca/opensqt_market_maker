# WebSocket Rate Limiting Spec

## Problem
The WebSocket server (`pkg/liveserver/server.go`) has no limits on:
1.  Total concurrent connections (Risk: Resource Exhaustion/DoS)
2.  Connection rate per IP (Risk: Connection Bombing/DoS)

## Solution
Implement a dual-layer protection system:
1.  **Global Connection Limit**: Semaphore-based cap on total active connections.
2.  **Per-IP Rate Limiting**: Token bucket limiter for new connection attempts per IP.

## Design

### `Server` Struct Update
```go
type Server struct {
    // ...
    maxConnections int
    connSemaphore  chan struct{}
    
    // Rate Limiting
    rateLimitEnabled bool
    ipLimiters       sync.Map // map[string]*rate.Limiter
    rateLimit        rate.Limit
    rateBurst        int
    
    // Metrics (if available, otherwise log)
}
```

### `NewServer` Update
Add configuration options for `MaxConnections` and `RateLimit`.

### `handleWebSocket` Update
1.  **Check IP Rate Limit**:
    - Extract IP from `RemoteAddr` (or `X-Forwarded-For` if behind proxy, but stick to `RemoteAddr` for simplicity/safety unless config enabled).
    - Get/Create limiter for IP.
    - If `!limiter.Allow()`, reject with 429.
2.  **Check Global Limit**:
    - Try to acquire semaphore (`connSemaphore <- struct{}{}`).
    - If channel full/default case, reject with 503.
    - Defer release of semaphore (`<-connSemaphore`).

### Cleanup
Rate limiters map needs periodic cleanup to avoid memory leak from transient IPs.
- Use a background goroutine that runs every minute and removes limiters not used in last 5 mins.
- Or simpler: Use a cache with TTL (e.g. `hashicorp/golang-lru` or similar), but standard map with cleanup is fine for MVP.

## Implementation Details

### File: `pkg/liveserver/server.go`

```go
// Add strict limits
const (
    DefaultMaxConnections = 1000
    DefaultRateLimit      = 10.0 // per second
    DefaultRateBurst      = 20
)

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    // 1. IP Rate Limit
    if s.rateLimitEnabled {
        ip := getClientIP(r)
        limiter := s.getIPLimiter(ip)
        if !limiter.Allow() {
            http.Error(w, "Too many requests", http.StatusTooManyRequests)
            return
        }
    }

    // 2. Global Connection Limit
    select {
    case s.connSemaphore <- struct{}{}:
        defer func() { <-s.connSemaphore }()
    default:
        http.Error(w, "Server busy", http.StatusServiceUnavailable)
        return
    }

    // 3. Upgrade
    conn, err := s.upgrader.Upgrade(w, r, nil)
    // ...
}
```

### File: `pkg/liveserver/limiter.go` (New)
Implement `IPLimiter` manager with cleanup logic.

## Verification
Create `pkg/liveserver/server_limit_test.go`:
1.  Test Global Limit: Configure max=10. Start 15 concurrent connections. 10 should succeed, 5 fail.
2.  Test Rate Limit: Configure 5/sec. Burst 10 requests. Verify rejection after burst.

## Acceptance Criteria
- Global limit enforced.
- Per-IP limit enforced.
- Tests pass.
