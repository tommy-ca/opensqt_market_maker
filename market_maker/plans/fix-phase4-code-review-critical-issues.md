# Fix Phase 4 Exchange Adapters - 12 Critical Code Review Issues

## Overview

This plan addresses 12 critical issues identified during comprehensive multi-agent code review of the `feature/phase4-exchange-adapters` branch. The issues span security vulnerabilities, data integrity risks, memory leaks, race conditions, and missing observability features in a high-frequency cryptocurrency trading system.

**Branch**: `feature/phase4-exchange-adapters`
**System**: Go-based market maker with 5 exchange integrations (Binance, OKX, Bybit, Gate.io, Bitget)
**Architecture**: gRPC microservices, SQLite state persistence, WebSocket real-time streams
**Criticality**: Production-blocking issues affecting financial safety and system stability

## Problem Statement

### Security & Compliance Risks
- **Missing .gitignore**: SQLite databases, credentials, and logs at risk of git commit exposure
- **Credential logging**: API keys partially exposed in authentication logs (internal/auth/interceptor.go:147-150)
- **No rate limiting**: WebSocket server vulnerable to DoS attacks

### Data Integrity Failures
- **Position reconciliation**: Detects divergence but never corrects (internal/risk/reconciler.go:194-209)
- **State persistence ordering**: In-memory updates before database commits create orphaned orders (internal/engine/simple/engine.go:228-232)
- **Race condition**: Reconciliation reads positions without locks during concurrent writes (internal/risk/reconciler.go:139-192)

### Memory & Resource Leaks
- **Goroutine leak**: WebSocket heartbeat goroutines never cleaned up (pkg/websocket/client.go:148-151)
- **Unbounded growth**: Error timestamps can grow to 6000+ entries during high error rates (internal/trading/order/executor.go:206)

### Code Quality & Maintainability
- **Type mismatch bug**: Method receiver is `*ParseError` instead of `*BaseAdapter` (internal/exchange/base/adapter.go:63)
- **Code duplication**: 750+ lines duplicated across 5 exchanges; BaseAdapter exists but unused
- **Missing APIs**: No gRPC services for risk monitoring or position introspection (0% agent accessibility)

## Motivation

**Why This Matters:**

1. **Financial Risk**: Position divergence and state corruption can lead to untracked positions, risk limit violations, and direct capital loss
2. **System Stability**: Memory leaks and goroutine accumulation cause eventual OOM crashes during high-volatility trading periods
3. **Security Compliance**: Credential exposure and logging violations fail PCI DSS, SOC 2 audits
4. **Operational Burden**: Code duplication makes bug fixes require 5x work; missing observability APIs require manual intervention
5. **Production Readiness**: Current implementation has 10 P1 blocking issues preventing safe production deployment

**Real-World Impact Examples:**
- **Position divergence**: System thinks it holds 10 BTC, exchange reports 12 BTC → places more orders → breaches risk limits → forced liquidation
- **State persistence failure**: Order placed in memory, SQLite write fails, system restarts → orphaned filled order → untracked position
- **Goroutine leak**: 100 reconnections/day × 5 exchanges × 7 days = 3500+ leaked goroutines → OOM crash during peak trading

## Proposed Solution

### High-Level Approach

**4-Phase Implementation Strategy:**

1. **Phase 1: Security Baseline** (Week 1) - Non-invasive, low-risk fixes
2. **Phase 2: Core Infrastructure** (Week 2) - Concurrency and resource management
3. **Phase 3: Trading Logic** (Week 3-4) - High-risk position reconciliation and state persistence
4. **Phase 4: Observability & Cleanup** (Week 5) - APIs, refactoring, monitoring

**Key Architectural Decisions:**

1. **Persist-First Pattern**: Database commits before in-memory updates (Issue #6)
2. **Snapshot Isolation**: Reconciliation uses position manager snapshot copies (Issue #7)
3. **Auto-Correction with Thresholds**: Reconciliation auto-corrects small divergence (\u003c5%), requires manual approval for large (Issue #5)
4. **Ring Buffer Eviction**: Error timestamps capped at 1000 entries with FIFO eviction (Issue #4)
5. **WaitGroup Tracking**: All goroutines tracked with sync.WaitGroup for graceful shutdown (Issue #3)

## Technical Approach

### Phase 1: Security Baseline (Week 1)

**Issues Addressed**: #2 (gitignore), #1 (type mismatch), #18 (log sanitization)

#### Task 1.1: Create Comprehensive .gitignore

**File**: `/home/tommyk/projects/quant/engine/opensqt_market_maker/market_maker/.gitignore`

**Implementation**:
```gitignore
# API Keys & Credentials
.env
.env.*
*.key
*.pem
*.p12
credentials.json
secrets.yaml

# Database Files
*.db
*.db-shm
*.db-wal
*.sqlite
*.sqlite3
data/*.db

# Logs (may contain trade data)
logs/
*.log
*.log.*

# Backups
backups/
*.backup
*.bak

# Position & State Files
positions.json
state.json
.state/

# Build Artifacts
bin/
dist/
market_maker
main

# IDE & OS
.vscode/
.idea/
*.swp
.DS_Store

# Test Data with Real Credentials
testdata/real_*
testdata/*.prod.*
```

**Validation**:
```bash
# Audit git history for leaked credentials
git log --all --full-history -- '*.env' '*.key' '*.db'

# If found, rotate ALL API keys immediately
# Consider git history rewrite for compliance
```

**Effort**: 2 hours
**Risk**: Low (additive change, no code impact)

---

#### Task 1.2: Fix BaseAdapter Type Mismatch Bug

**File**: `internal/exchange/base/adapter.go:63`

**Current Code** (Bug):
```go
func (b *ParseError) SetParseError(fn ParseErrorFunc) {
    b.parseError = fn
}
```

**Fixed Code**:
```go
func (b *BaseAdapter) SetParseError(fn ParseErrorFunc) {
    b.parseError = fn
}
```

**Testing**:
```go
// internal/exchange/base/adapter_test.go
func TestSetParseError(t *testing.T) {
    adapter := &BaseAdapter{}
    called := false

    adapter.SetParseError(func(err error) error {
        called = true
        return err
    })

    // Verify method is callable and sets field
    assert.NotNil(t, adapter.parseError)
}
```

**Effort**: 1 hour
**Risk**: Very Low (simple type fix)

---

#### Task 1.3: Sanitize Authentication Logs

**File**: `internal/auth/interceptor.go:147-150`

**Current Code** (Vulnerable):
```go
v.failureLogger.Warn("Authentication failed: invalid API key",
    "method", info.FullMethod,
    "key_prefix", maskAPIKey(apiKey))  // Exposes 8 chars

func maskAPIKey(apiKey string) string {
    return apiKey[:8] + "***"  // 25% of key exposed
}
```

**Fixed Code**:
```go
v.failureLogger.Warn("Authentication failed: invalid API key",
    "method", info.FullMethod,
    "client_ip", getClientIP(ctx),
    "request_id", getRequestID(ctx))  // No key logged

// Remove maskAPIKey entirely or use secure hash
func hashAPIKey(apiKey string) string {
    hash := sha256.Sum256([]byte(apiKey))
    return hex.EncodeToString(hash[:6])  // 12 hex chars for correlation
}
```

**Additional Changes**:
- Audit all logger calls for sensitive fields: `grep -rn "apiKey\|api_key\|secret" internal/ | grep -i "log"`
- Add pre-commit hook to detect credential logging patterns

**Testing**:
```go
func TestAuthLogsNoSensitiveData(t *testing.T) {
    // Capture logs
    logBuf := &bytes.Buffer{}
    logger := zerolog.New(logBuf)

    // Attempt auth with invalid key
    validator.Validate(ctx, "secret-api-key-12345")

    // Verify no key fragments in logs
    logOutput := logBuf.String()
    assert.NotContains(t, logOutput, "secret")
    assert.NotContains(t, logOutput, "12345")
}
```

**Effort**: 4 hours
**Risk**: Low (logging change only)

---

### Phase 2: Core Infrastructure (Week 2)

**Issues Addressed**: #19 (goroutine leak), #20 (unbounded growth), #23 (race condition)

#### Task 2.1: Fix WebSocket Heartbeat Goroutine Leak

**File**: `pkg/websocket/client.go:148-151`

**Current Code** (Leak):
```go
heartbeatCtx, heartbeatCancel := context.WithCancel(c.ctx)
if pingInterval > 0 {
    go c.heartbeat(heartbeatCtx, heartbeatCancel)  // No WaitGroup tracking
}
```

**Fixed Code**:
```go
type Client struct {
    // ... existing fields
    wg sync.WaitGroup  // Track all goroutines
}

func (c *Client) Connect(ctx context.Context) error {
    // ... existing connection logic

    // Track heartbeat goroutine
    if c.pingInterval > 0 {
        c.wg.Add(1)
        go func() {
            defer c.wg.Done()
            c.heartbeat(heartbeatCtx, heartbeatCancel)
        }()
    }

    // Track read loop goroutine
    c.wg.Add(1)
    go func() {
        defer c.wg.Done()
        c.readLoop()
    }()

    return nil
}

func (c *Client) Close() error {
    c.cancel()  // Signal all goroutines to stop

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
        c.logger.Warn("Some goroutines did not exit within timeout")
    }

    if c.conn != nil {
        return c.conn.Close()
    }
    return nil
}
```

**Testing**:
```go
func TestNoGoroutineLeak(t *testing.T) {
    before := runtime.NumGoroutine()

    client := NewClient(url, WithPingInterval(100*time.Millisecond))
    err := client.Connect(context.Background())
    require.NoError(t, err)

    time.Sleep(200 * time.Millisecond)

    err = client.Close()
    require.NoError(t, err)

    // Wait for cleanup
    time.Sleep(100 * time.Millisecond)

    after := runtime.NumGoroutine()
    assert.LessOrEqual(t, after, before+1, "Goroutine leak detected")
}

func TestReconnectionNoLeak(t *testing.T) {
    before := runtime.NumGoroutine()

    // Simulate 10 reconnection cycles
    for i := 0; i < 10; i++ {
        client := NewClient(url, WithPingInterval(100*time.Millisecond))
        client.Connect(context.Background())
        time.Sleep(50 * time.Millisecond)
        client.Close()
    }

    time.Sleep(200 * time.Millisecond)
    after := runtime.NumGoroutine()
    assert.LessOrEqual(t, after, before+2, "Leak from reconnections")
}
```

**Effort**: 6 hours
**Risk**: Medium (requires careful testing of shutdown paths)

---

#### Task 2.2: Implement Bounded Error Timestamp Storage

**File**: `internal/trading/order/executor.go:206`

**Current Code** (Unbounded):
```go
oe.errorTimestamps = append(oe.errorTimestamps, time.Now())  // No bounds
```

**Fixed Code (Ring Buffer)**:
```go
type OrderExecutor struct {
    errorTimestamps []time.Time
    errorIndex      int
    errorCapacity   int  // Max 1000 entries
    errorMu         sync.Mutex
}

func NewOrderExecutor(cfg *Config) *OrderExecutor {
    return &OrderExecutor{
        errorTimestamps: make([]time.Time, 0, 1000),
        errorCapacity:   1000,
    }
}

func (oe *OrderExecutor) recordError() {
    oe.errorMu.Lock()
    defer oe.errorMu.Unlock()

    if len(oe.errorTimestamps) < oe.errorCapacity {
        // Still growing, append normally
        oe.errorTimestamps = append(oe.errorTimestamps, time.Now())
    } else {
        // At capacity, overwrite oldest entry (ring buffer)
        oe.errorTimestamps[oe.errorIndex] = time.Now()
        oe.errorIndex = (oe.errorIndex + 1) % oe.errorCapacity
    }
}

func (oe *OrderExecutor) getRecentErrors() int {
    oe.errorMu.Lock()
    defer oe.errorMu.Unlock()

    cutoff := time.Now().Add(-5 * time.Minute)
    count := 0
    for _, t := range oe.errorTimestamps {
        if t.After(cutoff) {
            count++
        }
    }
    return count
}
```

**Testing**:
```go
func TestErrorTrackingBounded(t *testing.T) {
    executor := NewOrderExecutor(cfg)

    // Simulate 10,000 errors
    for i := 0; i < 10000; i++ {
        executor.recordError()
    }

    // Verify bounded growth
    executor.errorMu.Lock()
    count := len(executor.errorTimestamps)
    executor.errorMu.Unlock()

    assert.LessOrEqual(t, count, 1000, "Error tracking unbounded")
}

func BenchmarkRecordError(b *testing.B) {
    executor := NewOrderExecutor(cfg)
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            executor.recordError()
        }
    })
}
```

**Effort**: 4 hours
**Risk**: Low (self-contained change)

---

#### Task 2.3: Fix Reconciliation Race Condition

**File**: `internal/risk/reconciler.go:139-192`

**Current Code** (Race):
```go
func (r *Reconciler) reconcileOrders(ctx context.Context, slots map[string]*core.InventorySlot, exchangeOrders []*pb.Order) {
    for _, slot := range slots {  // ⚠️ Reading slots without lock
        localOrderMap[slot.OrderId] = slot
    }
}
```

**Fixed Code (Snapshot Pattern)**:
```go
// Add to PositionManager
func (m *PositionManager) CreateReconciliationSnapshot() map[string]*core.InventorySlot {
    m.mu.RLock()
    defer m.mu.RUnlock()

    // Deep copy all slots
    snapshot := make(map[string]*core.InventorySlot, len(m.slots))
    for k, v := range m.slots {
        slotCopy := *v  // Copy struct
        snapshot[k] = &slotCopy
    }

    return snapshot
}

// Update Reconciler
func (r *Reconciler) runReconciliation(ctx context.Context) error {
    // Get immutable snapshot (no races)
    slots := r.positionManager.CreateReconciliationSnapshot()

    // Fetch exchange state
    exchangeOrders, err := r.exchange.GetOpenOrders(ctx)
    if err != nil {
        return err
    }

    // Reconcile using snapshot
    r.reconcileOrders(ctx, slots, exchangeOrders)

    return nil
}
```

**Testing**:
```go
func TestReconciliationNoRace(t *testing.T) {
    posManager := NewPositionManager()
    reconciler := NewReconciler(posManager, ...)

    // Start concurrent order updates
    go func() {
        for i := 0; i < 1000; i++ {
            posManager.OnOrderUpdate(ctx, &pb.OrderUpdate{
                OrderId: fmt.Sprintf("order-%d", i%10),
                FilledQuantity: decimal.NewFromInt(int64(i)),
            })
            time.Sleep(time.Microsecond)
        }
    }()

    // Run reconciliation concurrently
    for i := 0; i < 100; i++ {
        reconciler.runReconciliation(ctx)
        time.Sleep(time.Millisecond)
    }

    // If compiled with -race, this will fail if races exist
}

// Run with: go test -race ./internal/risk/
```

**Effort**: 6 hours
**Risk**: Medium (requires understanding position manager internals)

---

### Phase 3: Trading Logic (Week 3-4)

**Issues Addressed**: #21 (reconciliation correction), #22 (state persistence ordering)

#### Task 3.1: Implement Position Reconciliation Auto-Correction

**File**: `internal/risk/reconciler.go:194-209`

**Current Code** (No Action):
```go
if !localSize.Equal(exchangeSize) {
    r.logger.Warn("Position mismatch detected",
        "local_size", localSize,
        "exchange_size", exchangeSize)
    // ⚠️ NO CORRECTIVE ACTION
}
```

**Fixed Code (Auto-Correction with Thresholds)**:
```go
func (r *Reconciler) reconcileOrders(ctx context.Context, slots map[string]*core.InventorySlot, exchangeOrders []*pb.Order) {
    // ... existing comparison logic

    if !localSize.Equal(exchangeSize) {
        divergence := exchangeSize.Sub(localSize)
        divergencePct := divergence.Div(exchangeSize).Mul(decimal.NewFromInt(100)).Abs()

        r.logger.Warn("Position mismatch detected",
            "symbol", symbol,
            "local_size", localSize,
            "exchange_size", exchangeSize,
            "divergence", divergence,
            "divergence_pct", divergencePct)

        // Emit metrics
        r.metrics.PositionDivergence.WithLabelValues(symbol).Set(divergence.InexactFloat64())

        // CORRECTIVE ACTION based on divergence size
        if divergencePct.LessThan(decimal.NewFromFloat(5.0)) {
            // Small divergence (<5%) - Auto-correct
            r.logger.Info("Auto-correcting small position divergence")
            if err := r.positionManager.ForceSync(ctx, symbol, exchangeSize); err != nil {
                r.logger.Error("Failed to sync position", "error", err)
                return
            }
            r.metrics.PositionCorrections.WithLabelValues(symbol, "auto").Inc()
        } else {
            // Large divergence (≥5%) - Halt trading and alert
            r.logger.Error("CRITICAL: Large position divergence detected - halting trading",
                "symbol", symbol,
                "divergence_pct", divergencePct)

            // Circuit breaker - stop trading for this symbol
            if err := r.circuitBreaker.Open(symbol, "position_divergence"); err != nil {
                r.logger.Error("Failed to open circuit breaker", "error", err)
            }

            // Alert operations team
            r.alertManager.Send(Alert{
                Severity: "CRITICAL",
                Title:    "Position Divergence Detected",
                Message:  fmt.Sprintf("Symbol %s: local=%s, exchange=%s, divergence=%s%%",
                    symbol, localSize, exchangeSize, divergencePct),
                Action:   "Manual investigation required - trading halted",
            })

            r.metrics.PositionCorrections.WithLabelValues(symbol, "manual_required").Inc()
        }
    }
}

// Add to PositionManager
func (m *PositionManager) ForceSync(ctx context.Context, symbol string, exchangePosition decimal.Decimal) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    key := PositionKey{Symbol: symbol}
    currentPosition := m.positions[key]

    adjustment := exchangePosition.Sub(currentPosition.Quantity)

    m.logger.Warn("Force syncing position from reconciliation",
        "symbol", symbol,
        "old_quantity", currentPosition.Quantity,
        "new_quantity", exchangePosition,
        "adjustment", adjustment)

    // Update position
    currentPosition.Quantity = exchangePosition
    currentPosition.UpdatedAt = time.Now()

    // Persist to database
    if err := m.store.SavePosition(ctx, currentPosition); err != nil {
        return fmt.Errorf("failed to persist position sync: %w", err)
    }

    return nil
}
```

**Testing**:
```go
func TestSmallDivergenceAutoCorrects(t *testing.T) {
    reconciler := setupReconciler(t)

    // Create 3% divergence
    reconciler.positionManager.SetPosition("BTC", decimal.NewFromInt(100))
    mockExchange.SetPosition("BTC", decimal.NewFromInt(103))

    err := reconciler.runReconciliation(ctx)
    require.NoError(t, err)

    // Verify auto-correction
    pos := reconciler.positionManager.GetPosition("BTC")
    assert.Equal(t, "103", pos.Quantity.String())

    // Verify metric
    assert.Equal(t, 1, reconciler.metrics.PositionCorrections["auto"])
}

func TestLargeDivergenceHaltsTrading(t *testing.T) {
    reconciler := setupReconciler(t)

    // Create 10% divergence
    reconciler.positionManager.SetPosition("BTC", decimal.NewFromInt(100))
    mockExchange.SetPosition("BTC", decimal.NewFromInt(110))

    err := reconciler.runReconciliation(ctx)
    require.NoError(t, err)

    // Verify NO auto-correction
    pos := reconciler.positionManager.GetPosition("BTC")
    assert.Equal(t, "100", pos.Quantity.String())

    // Verify circuit breaker opened
    assert.True(t, reconciler.circuitBreaker.IsOpen("BTC"))

    // Verify alert sent
    assert.Equal(t, 1, len(reconciler.alertManager.SentAlerts))
}
```

**Effort**: 12 hours
**Risk**: High (affects trading logic, requires careful threshold tuning)

---

#### Task 3.2: Fix State Persistence Ordering

**File**: `internal/engine/simple/engine.go:228-232`

**Current Code** (Unsafe):
```go
// Update in-memory state
if err := e.positionManager.OnOrderUpdate(ctx, update); err != nil {
    return err
}

// Save to database
if err := e.store.SaveState(ctx, newState); err != nil {
    e.logger.Error("Failed to save state", "error", err)
    return err  // ⚠️ State already changed in memory!
}
```

**Fixed Code (Persist-First Pattern)**:
```go
func (e *SimpleEngine) processOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
    // 1. Build new state snapshot
    newState, err := e.buildStateSnapshot(ctx)
    if err != nil {
        return fmt.Errorf("failed to build state snapshot: %w", err)
    }

    // 2. Apply update to snapshot (not to live state yet)
    if err := applyUpdateToSnapshot(newState, update); err != nil {
        return fmt.Errorf("failed to apply update to snapshot: %w", err)
    }

    // 3. Persist to database FIRST
    if err := e.store.SaveState(ctx, newState); err != nil {
        e.logger.Error("Failed to save state", "error", err)
        return fmt.Errorf("state persistence failed: %w", err)
        // In-memory state NOT changed yet - safe to return error
    }

    // 4. ONLY AFTER successful persistence, update in-memory state
    if err := e.positionManager.OnOrderUpdate(ctx, update); err != nil {
        // This should never fail if snapshot application succeeded
        e.logger.Error("CRITICAL: In-memory update failed after persistence", "error", err)

        // Trigger emergency reconciliation on next run
        e.forceReconciliationFlag.Store(true)

        return fmt.Errorf("critical: state desync: %w", err)
    }

    return nil
}

// Helper to apply update to state snapshot
func applyUpdateToSnapshot(state *pb.State, update *pb.OrderUpdate) error {
    // Find or create position entry in snapshot
    for _, pos := range state.Positions {
        if pos.Symbol == update.Symbol {
            pos.Quantity = update.NewQuantity
            pos.UpdatedAt = time.Now().Unix()
            return nil
        }
    }

    // New position
    state.Positions = append(state.Positions, &pb.Position{
        Symbol:    update.Symbol,
        Quantity:  update.NewQuantity,
        UpdatedAt: time.Now().Unix(),
    })

    return nil
}
```

**Testing**:
```go
func TestStatePersistenceFailureSafe(t *testing.T) {
    // Use mock store that fails on write
    failStore := &FailingStore{}
    engine := NewSimpleEngine(failStore, ...)

    // Get initial position
    initialPos := engine.positionManager.GetPosition("BTC")

    // Process order update (should fail on persistence)
    err := engine.processOrderUpdate(ctx, &pb.OrderUpdate{
        Symbol: "BTC",
        NewQuantity: decimal.NewFromInt(150),
    })

    // Persistence should fail
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "state persistence failed")

    // In-memory position should NOT change
    currentPos := engine.positionManager.GetPosition("BTC")
    assert.Equal(t, initialPos.Quantity, currentPos.Quantity,
        "Position changed despite persistence failure")
}

func TestCrashRecovery(t *testing.T) {
    // 1. Start engine
    engine := NewSimpleEngine(...)
    engine.processOrderUpdate(ctx, &pb.OrderUpdate{
        Symbol: "BTC",
        NewQuantity: decimal.NewFromInt(100),
    })
    engine.processOrderUpdate(ctx, &pb.OrderUpdate{
        Symbol: "BTC",
        NewQuantity: decimal.NewFromInt(150),
    })

    // 2. Simulate crash (no cleanup)
    engine = nil

    // 3. Restart engine
    newEngine := NewSimpleEngine(...)

    // 4. Verify position matches last persisted state
    pos := newEngine.positionManager.GetPosition("BTC")
    assert.Equal(t, "150", pos.Quantity.String())
}
```

**Effort**: 16 hours
**Risk**: Very High (core trading logic, requires extensive testing)

---

### Phase 4: Observability & Cleanup (Week 5)

**Issues Addressed**: #17 (rate limiting), #24 (risk API), #25 (position API), #15 (code duplication)

#### Task 4.1: Add WebSocket Rate Limiting

**File**: `pkg/liveserver/server.go`

**Implementation**:
```go
import "golang.org/x/time/rate"

type Server struct {
    // ... existing fields
    maxConnections int
    activeConns    atomic.Int32
    connLimiter    chan struct{}
    rateLimiters   sync.Map  // map[string]*rate.Limiter per connection
}

func NewServer(opts ...Option) *Server {
    s := &Server{
        maxConnections: 1000,
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

    // Get per-connection rate limiter
    connID := r.RemoteAddr
    limiter := s.getRateLimiter(connID)

    conn, err := s.upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    defer conn.Close()

    s.activeConns.Add(1)
    defer s.activeConns.Add(-1)

    // Message loop with rate limiting
    for {
        if !limiter.Allow() {
            s.logger.Warn("Rate limit exceeded", "conn", connID)
            continue
        }

        _, message, err := conn.ReadMessage()
        if err != nil {
            break
        }

        s.handleMessage(conn, message)
    }
}

func (s *Server) getRateLimiter(connID string) *rate.Limiter {
    if limiter, ok := s.rateLimiters.Load(connID); ok {
        return limiter.(*rate.Limiter)
    }

    limiter := rate.NewLimiter(rate.Limit(100), 200)  // 100 msg/s, burst 200
    s.rateLimiters.Store(connID, limiter)
    return limiter
}
```

**Testing**: Load test with 10,000 connections, verify rate limiting active

**Effort**: 8 hours
**Risk**: Medium (production traffic impact)

---

#### Task 4.2: Add Risk Monitoring gRPC API

**File**: `internal/risk/grpc_service.go` (new), `api/proto/risk.proto` (new)

**Protobuf Definition**:
```protobuf
service RiskService {
    rpc GetRiskMetrics(GetRiskMetricsRequest) returns (GetRiskMetricsResponse);
    rpc GetPositionLimits(GetPositionLimitsRequest) returns (GetPositionLimitsResponse);
    rpc SubscribeRiskAlerts(SubscribeRiskAlertsRequest) returns (stream RiskAlert);
}

message GetRiskMetricsRequest {
    repeated string symbols = 1;
}

message GetRiskMetricsResponse {
    repeated SymbolRiskMetrics metrics = 1;
}

message SymbolRiskMetrics {
    string symbol = 1;
    string position_size = 2;
    string notional_value = 3;
    string unrealized_pnl = 4;
    double leverage = 5;
    double risk_score = 6;
    bool limit_breach = 7;
}
```

**Implementation**: See TODO #024 for full implementation details

**Effort**: 14 hours
**Risk**: Low (new API, no existing dependencies)

---

#### Task 4.3: Refactor BaseAdapter Code Duplication

**Files**: All exchange adapters in `internal/exchange/`

**Strategy**: Incremental migration, one exchange at a time

**Week 5 Scope**: Migrate Binance adapter as pilot (validates pattern)
**Future Work**: Migrate remaining 4 exchanges in subsequent sprints

**Effort**: 20 hours (Binance only)
**Risk**: Medium (requires careful validation of behavioral equivalence)

---

## Implementation Phases

### Phase 1: Security Baseline (Week 1)
**Deliverables**:
- [ ] `.gitignore` created with comprehensive patterns
- [ ] Git history audited for leaked credentials
- [ ] BaseAdapter type mismatch fixed
- [ ] Authentication logging sanitized
- [ ] All Phase 1 tests passing

**Risk Level**: ⬜ Low
**Can Deploy to Production**: ✅ Yes (independent changes)

---

### Phase 2: Core Infrastructure (Week 2)
**Deliverables**:
- [ ] WebSocket goroutine leak fixed with WaitGroup
- [ ] Error timestamp storage bounded (ring buffer)
- [ ] Reconciliation race condition fixed (snapshot pattern)
- [ ] All Phase 2 tests passing with `-race` flag
- [ ] 24-hour soak test shows stable goroutine count and memory

**Risk Level**: 🟨 Medium
**Can Deploy to Production**: ⚠️ With Staging Validation
**Prerequisite**: Phase 1 complete

---

### Phase 3: Trading Logic (Week 3-4)
**Deliverables**:
- [ ] Position reconciliation auto-correction implemented
- [ ] Auto-correction thresholds configured (5% cutoff)
- [ ] Circuit breaker integration for large divergence
- [ ] Alert manager notifications working
- [ ] State persistence ordering fixed (persist-first pattern)
- [ ] Crash recovery tests passing
- [ ] Reconciliation correction tests passing
- [ ] 72-hour staging validation with real exchanges (testnet)

**Risk Level**: 🟥 High
**Can Deploy to Production**: ⚠️ Canary Deployment Required
**Prerequisites**: Phase 1 + Phase 2 complete

---

### Phase 4: Observability & Cleanup (Week 5)
**Deliverables**:
- [ ] WebSocket rate limiting implemented
- [ ] Risk monitoring gRPC API deployed
- [ ] Position manager introspection API deployed
- [ ] Binance adapter migrated to BaseAdapter pattern
- [ ] All API integration tests passing
- [ ] Load testing complete (10K connections, rate limiting validated)

**Risk Level**: 🟨 Medium
**Can Deploy to Production**: ✅ Yes
**Prerequisites**: Phase 3 complete

---

## Acceptance Criteria

### Functional Requirements

**Security**:
- [ ] No credentials in git history or working tree
- [ ] Authentication logs contain no API key fragments
- [ ] WebSocket connections rate-limited to 100 msg/s per connection
- [ ] Maximum 1000 concurrent WebSocket connections enforced

**Data Integrity**:
- [ ] Position divergence \<5% auto-corrects within 60 seconds
- [ ] Position divergence ≥5% opens circuit breaker and alerts operations
- [ ] State persistence failures do NOT change in-memory state
- [ ] Crash recovery restores correct last-persisted state
- [ ] No race conditions detected with `go test -race`

**Resource Management**:
- [ ] Goroutine count stable over 24 hours (max growth \<5%)
- [ ] Error timestamp storage capped at 1000 entries
- [ ] Memory growth \<10 MB over 24 hours under normal load
- [ ] WebSocket connection limit prevents OOM

**Observability**:
- [ ] Risk monitoring API exposes position sizes, PnL, risk scores
- [ ] Position manager API exposes current positions and pending orders
- [ ] Alert manager sends critical divergence notifications
- [ ] Prometheus metrics track reconciliation corrections, divergence amounts

### Non-Functional Requirements

**Performance**:
- [ ] State persistence adds \<5ms latency to order processing
- [ ] Reconciliation snapshot adds \<10ms overhead
- [ ] Auto-correction completes within 2 seconds
- [ ] gRPC API query latency \<100ms p99

**Testing**:
- [ ] Unit test coverage ≥80% for all changed files
- [ ] Integration tests cover all critical paths
- [ ] Race detector clean (`go test -race`)
- [ ] Load tests validate bounded resource usage
- [ ] Chaos tests validate recovery from failures

**Documentation**:
- [ ] Reconciliation correction runbook created
- [ ] State persistence failure recovery procedure documented
- [ ] Lock hierarchy and deadlock avoidance guide updated
- [ ] API documentation generated for new gRPC services
- [ ] Deployment rollback plan documented

---

## Success Metrics

**Pre-Deployment (Staging)**:
- Zero race conditions detected over 72-hour run
- Zero goroutine leaks over 10 reconnection cycles
- Zero position divergence incidents or 100% auto-correction success
- Zero state persistence failures leaving inconsistent state

**Post-Deployment (Production)**:
- 99.9% uptime (max 43 minutes downtime/month)
- \<1% position divergence rate (target: 0%)
- Mean time to auto-correct divergence: \<60 seconds
- Zero credential exposure incidents
- Memory growth \<5% per week

**Observability**:
- 100% of position divergence incidents logged and alerted
- 100% of reconciliation corrections tracked in metrics
- API query success rate \u003e99.5%

---

## Dependencies & Prerequisites

### External Dependencies
- **SQLite 3.x** with WAL mode support
- **Go 1.24+** for atomic operations
- **gRPC 1.x** for API implementation
- **Prometheus client library** for metrics

### Internal Dependencies
**Blockers**:
- Issue #7 (race condition fix) MUST complete before #5 (reconciliation correction)
- Issue #6 (persistence ordering) MUST complete before production deployment of Phase 3

**Optional**:
- Alert manager integration (can use log-based alerts as fallback)
- Circuit breaker implementation (can use manual trading halt as fallback)

### Database Schema Changes
```sql
-- Optional: Add reconciliation audit table
CREATE TABLE IF NOT EXISTS reconciliation_corrections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol TEXT NOT NULL,
    local_position TEXT NOT NULL,
    exchange_position TEXT NOT NULL,
    divergence TEXT NOT NULL,
    action TEXT NOT NULL,  -- 'auto' or 'manual_required'
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_reconciliation_symbol ON reconciliation_corrections(symbol);
CREATE INDEX idx_reconciliation_created_at ON reconciliation_corrections(created_at);
```

---

## Risk Analysis & Mitigation

### High-Risk Changes

**Risk 1: State Persistence Ordering Change (Issue #6)**
- **Impact**: Core trading logic, wrong implementation = data loss
- **Probability**: Medium (complex transactional logic)
- **Mitigation**:
  - Implement in separate branch with extensive integration tests
  - Run 72-hour staging test with testnet exchanges
  - Canary deployment to 1 exchange before full rollout
  - Keep rollback plan ready (revert to old persistence logic)
  - Add monitoring for persistence failure rate

**Risk 2: Position Reconciliation Auto-Correction (Issue #5)**
- **Impact**: Incorrect correction = wrong trades, financial loss
- **Probability**: Medium (threshold tuning required)
- **Mitigation**:
  - Start with 1% auto-correction threshold (conservative)
  - Require manual approval for first 10 corrections
  - Monitor correction accuracy for 1 week before increasing threshold
  - Add kill switch to disable auto-correction
  - Log all corrections for post-incident analysis

**Risk 3: Race Condition Fix Breaking Performance (Issue #7)**
- **Impact**: Lock contention slows order processing
- **Probability**: Low (snapshot pattern minimizes lock time)
- **Mitigation**:
  - Benchmark before/after with high-frequency order stream
  - Use RWMutex (not Mutex) to allow concurrent reads
  - Snapshot creation time \<10ms (measured in tests)
  - Add lock contention metrics to Prometheus

### Medium-Risk Changes

**Risk 4: WebSocket Goroutine Leak Fix (Issue #3)**
- **Impact**: Shutdown coordination failure = stuck goroutines
- **Probability**: Low (WaitGroup is well-tested pattern)
- **Mitigation**:
  - Add timeout to WaitGroup.Wait() (5 seconds)
  - Log warning if goroutines don't exit cleanly
  - Unit tests with high reconnection rate
  - Monitor goroutine count in production

**Risk 5: gRPC API Changes (Issues #24, #25)**
- **Impact**: API consumers break if incompatible changes
- **Probability**: Low (new APIs, no existing consumers)
- **Mitigation**:
  - Version APIs (v1 suffix)
  - Generate client SDKs for major languages
  - Document breaking changes in release notes
  - Provide migration guide for future changes

---

## Resource Requirements

### Team
- **Lead Developer**: 1 FTE for 5 weeks (full implementation)
- **Code Reviewer**: 0.3 FTE for 5 weeks (review cycles)
- **QA Engineer**: 0.5 FTE weeks 3-5 (integration testing)
- **DevOps Engineer**: 0.2 FTE weeks 3-5 (deployment support)

### Infrastructure
- **Staging Environment**: AWS/GCP instance matching production specs
- **Testnet Exchange Accounts**: Binance, OKX, Bybit testnet access
- **Monitoring**: Prometheus + Grafana for metrics dashboards
- **Alerting**: PagerDuty or Opsgenie for critical alerts

### Tools
- **Go 1.24+** with race detector
- **Buf** for protobuf compilation
- **goleak** for goroutine leak detection
- **go-sqlmock** for database testing
- **testify** for assertions

---

## Future Considerations

### Extensibility
- **Additional Exchanges**: BaseAdapter pattern enables faster integration (issue #15 complete)
- **Advanced Reconciliation**: Machine learning for divergence prediction
- **Real-time Risk Dashboard**: WebSocket streaming to web UI using position API
- **Automated Backtesting**: Use state persistence snapshots for strategy validation

### Scalability
- **Horizontal Scaling**: gRPC APIs enable load balancing across multiple instances
- **Database Migration**: SQLite → PostgreSQL when concurrent writes exceed SQLite limits
- **Caching Layer**: Redis for high-frequency position queries
- **Event Streaming**: Kafka for order update distribution to multiple consumers

### Monitoring Evolution
- **Distributed Tracing**: OpenTelemetry integration for request flow visualization
- **Anomaly Detection**: ML-based detection of unusual trading patterns
- **Capacity Planning**: Trend analysis for resource growth prediction
- **SLO/SLA Tracking**: Error budgets for position accuracy, uptime, latency

---

## Documentation Plan

### Code Documentation
- [ ] Package-level docs for `internal/risk/` explaining reconciliation logic
- [ ] Inline comments for complex algorithms (ring buffer, snapshot isolation)
- [ ] Architecture Decision Records (ADRs) for persist-first pattern, auto-correction thresholds
- [ ] API reference docs auto-generated from protobuf comments

### Operational Documentation
- [ ] **Runbook**: Position divergence response procedure
  - When to approve manual corrections
  - How to interpret circuit breaker alerts
  - Rollback procedure for failed deployments
- [ ] **Deployment Guide**: Phase-by-phase rollout instructions
- [ ] **Monitoring Guide**: Key metrics to watch, alert thresholds
- [ ] **Troubleshooting Guide**: Common issues and solutions
  - Goroutine leak diagnosis
  - State persistence failure recovery
  - Race condition debugging

### Developer Documentation
- [ ] **Contributing Guide**: Lock hierarchy rules, testing requirements
- [ ] **Testing Guide**: Running race detector, chaos tests, load tests
- [ ] **Architecture Overview**: Updated diagrams showing reconciliation flow, persistence ordering
- [ ] **API Client Examples**: Python, Go, TypeScript snippets for new gRPC APIs

---

## References & Research

### Internal References
- Code Review TODOs: `todos/014-pending-p1-base-adapter-type-mismatch-bug.md` through `todos/025-pending-p2-position-introspection-api.md`
- Architecture Documentation: `archive/ARCHITECTURE.md` (lock ordering hierarchy at lines 3-15)
- Repository Structure: `internal/trading/position/manager.go` (SuperPositionManager)
- Existing Tests: `internal/trading/order/executor_test.go`, `internal/risk/reconciler_test.go`

### External References
- **gRPC Patterns**: https://grpc.io/docs/guides/
  - Streaming RPC best practices
  - Interceptor design patterns
  - Health checking
- **Gorilla WebSocket**: https://context7.com/gorilla/websocket
  - Origin validation security
  - Rate limiting patterns
  - Heartbeat/keepalive
- **Prometheus Best Practices**: https://prometheus.io/docs/practices/
  - Metric types (Counter, Gauge, Histogram)
  - Label cardinality management
  - Custom collectors
- **Go Concurrency**: https://go.dev/blog/
  - Context cancellation patterns
  - WaitGroup lifecycle management
  - RWMutex vs atomic operations
- **SQLite WAL Mode**: https://www.sqlite.org/wal.html
  - Concurrent access patterns
  - Transaction isolation levels
  - Checkpointing behavior

### Related Work
- Previous PRs: Search for `position reconciliation`, `state persistence`, `goroutine leak` fixes in git history
- Similar Systems: Study exchange adapter patterns in `ccxt` (Python), `Freqtrade` (Python), `Hummingbot` (Python/Cython)
- Industry Standards: FIX protocol for position reconciliation, OWASP secure logging guidelines

---

## Appendix: Testing Strategy

### Unit Tests (Per-Task)
```bash
# Run with race detector
go test -race -v ./internal/...

# Run with coverage
go test -coverprofile=coverage.out ./internal/...
go tool cover -html=coverage.out

# Run specific test
go test -run TestReconciliationNoRace -race ./internal/risk/
```

### Integration Tests (End-to-End)
```bash
# Full system test with mock exchanges
go test -tags=integration ./tests/integration/

# Testnet test with real exchanges
TESTNET=1 go test -tags=integration ./tests/integration/reconciliation_test.go
```

### Load Tests
```bash
# Goroutine leak test (100 reconnections)
go test -run TestReconnectionNoLeak -count=100 ./pkg/websocket/

# Error storage growth test (10,000 errors)
go test -run TestErrorTrackingBounded ./internal/trading/order/

# WebSocket connection limit (10,000 connections)
go test -tags=loadtest -run TestConnectionLimit ./pkg/liveserver/
```

### Chaos Tests
```bash
# Kill SQLite during persistence
go test -run TestCrashRecovery ./internal/engine/simple/

# Network faults during reconciliation
go test -run TestNetworkFaultRecovery ./internal/risk/

# Race condition stress test
go test -race -run TestReconciliationUnderLoad -timeout=30m ./internal/risk/
```

### Soak Tests (Staging Environment)
```bash
# 24-hour run with metrics collection
./bin/market_maker --config=config.staging.yaml

# Monitor:
# - Goroutine count: curl localhost:6060/debug/pprof/goroutine
# - Memory usage: curl localhost:6060/debug/pprof/heap
# - Prometheus metrics: curl localhost:9090/metrics
```

---

## Appendix: Deployment Checklist

### Pre-Deployment
- [ ] All Phase 1-4 tests passing
- [ ] Code review approved by 2+ engineers
- [ ] Staging environment validated for 72 hours
- [ ] Rollback plan documented and tested
- [ ] Monitoring dashboards created
- [ ] Alert rules configured and tested
- [ ] Oncall engineer briefed on changes
- [ ] Deployment window scheduled (low-trading hours)

### Deployment Steps
1. [ ] Deploy to staging, run smoke tests
2. [ ] Deploy Phase 1 to production (security baseline)
3. [ ] Wait 24 hours, monitor for issues
4. [ ] Deploy Phase 2 to production (infrastructure)
5. [ ] Wait 48 hours, run load tests
6. [ ] Deploy Phase 3 to production (trading logic) - **CANARY**
   - Deploy to 1 exchange (Binance) first
   - Monitor for 72 hours
   - If stable, deploy to remaining exchanges
7. [ ] Deploy Phase 4 to production (observability)

### Post-Deployment
- [ ] Verify all metrics reporting correctly
- [ ] Verify alerts triggering as expected
- [ ] Run synthetic transactions to validate flows
- [ ] Monitor error rates for 7 days
- [ ] Document any unexpected behaviors
- [ ] Update runbooks based on production learnings

### Rollback Triggers
- Position divergence rate \u003e5% sustained for \u003e1 hour
- Goroutine count growth \u003e50% in 24 hours
- Memory growth \u003e100 MB in 24 hours
- Error rate increase \u003e2x baseline
- Circuit breaker openings \u003e5 per day
- State persistence failure rate \u003e1%

---

**Plan Version**: 1.0
**Last Updated**: 2026-01-23
**Estimated Completion**: 5 weeks from start
**Total Effort**: ~150-180 developer hours