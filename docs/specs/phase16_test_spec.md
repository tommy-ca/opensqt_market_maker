# Phase 16: gRPC Architecture - Test Specification

**Document Version**: 1.0  
**Last Updated**: January 22, 2026  
**Status**: In Progress  
**Author**: OpenSQT Team

---

## 1. Executive Summary

This document defines the comprehensive test strategy for validating the Phase 16 gRPC Architecture Migration. The goal is to ensure that the new gRPC-based communication between `exchange_connector`, `market_maker`, and `live_server` is correct, performant, and production-ready.

### Test Pyramid

```
           /\
          /E2E\           1-2 tests (Full system)
         /------\
        /Integration\     5-10 tests (Component interaction)
       /------------\
      /  Unit Tests  \    15-20 tests (Individual functions)
     /----------------\
```

### Coverage Goals

| Test Type | Target Coverage | Critical Path Coverage |
|-----------|----------------|------------------------|
| Unit Tests | 80% | 100% |
| Integration Tests | 70% | 100% |
| E2E Tests | N/A | 100% |

---

## 2. Test Strategy

### 2.1 Test-Driven Development (TDD) Approach

We follow a strict TDD methodology:

1. **Write test specification** (this document)
2. **Write failing tests** (red phase)
3. **Run tests to confirm failure** (validates test correctness)
4. **Implement code** (green phase)
5. **Run tests to confirm pass** (validates implementation)
6. **Refactor** (improve code quality)
7. **Repeat** (for next feature)

### 2.2 Test Levels

**Level 1: Unit Tests**
- Test individual functions in isolation
- Mock all external dependencies
- Fast execution (<100ms per test)
- No network I/O

**Level 2: Integration Tests**
- Test component interaction
- Real gRPC connections (localhost)
- Mock exchange data sources
- Medium execution (1-5s per test)

**Level 3: E2E Tests**
- Test complete system flow
- All three binaries running
- Mock exchange connector
- Slow execution (10-30s per test)

**Level 4: Performance Tests**
- Benchmark latency/throughput
- Memory profiling
- Load testing
- Long-running tests (minutes to hours)

### 2.3 Test Environment

**Required Infrastructure**:
- Go 1.21+ test runner
- gRPC localhost connections (ports 50051-50060)
- Mock exchange implementations
- Docker (for E2E tests)
- Performance profiling tools (pprof)

**Test Isolation**:
- Each test uses unique port
- Cleanup resources in `defer` statements
- Use `t.Parallel()` where possible
- Reset global state between tests

---

## 3. Unit Tests

### 3.1 RemoteExchange Client Tests

**File**: `market_maker/internal/exchange/remote_test.go`

#### Test: NewRemoteExchange

```go
func TestNewRemoteExchange(t *testing.T) {
    // Test successful creation
    // Test with invalid gRPC address
    // Test with connection timeout
    // Test with TLS configuration
}
```

**Scenarios**:
- Create client with valid address → success
- Create with invalid address → error
- Create with empty address → error
- Create with TLS cert → connection established

#### Test: GetAccount

```go
func TestRemoteExchange_GetAccount(t *testing.T) {
    // Test successful account fetch
    // Test server error handling
    // Test context cancellation
    // Test timeout
}
```

**Scenarios**:
- Call GetAccount → receives protobuf Account
- Server returns error → client receives error
- Context cancelled before response → context.Canceled
- Request timeout → DeadlineExceeded

### 3.2 ExchangeServer Tests

**File**: `market_maker/internal/exchange/server_test.go`

#### Test: Server Lifecycle

```go
func TestExchangeServer_Lifecycle(t *testing.T) {
    // Test server start
    // Test server stop
    // Test graceful shutdown
}
```

**Scenarios**:
- Start server → binds to port
- Stop server → port released
- Shutdown with active clients → clients notified
- Double stop → no panic

---

## 4. Integration Tests

### 4.1 RemoteExchange Streaming Tests

**File**: `market_maker/internal/exchange/remote_grpc_test.go`

#### Test: StartAccountStream

```go
func TestRemoteExchange_StartAccountStream(t *testing.T) {
    t.Run("successful_subscription", func(t *testing.T) {
        // 1. Start mock gRPC server
        // 2. Create RemoteExchange client
        // 3. Subscribe to account stream
        // 4. Mock server sends 5 account updates
        // 5. Verify callback receives all 5 updates
        // 6. Cancel context
        // 7. Verify goroutine exits
    })
    
    t.Run("server_error_handling", func(t *testing.T) {
        // Server sends error mid-stream
        // Verify client logs error
        // Verify callback not called with invalid data
    })
    
    t.Run("context_cancellation", func(t *testing.T) {
        // Start stream
        // Cancel context immediately
        // Verify goroutine exits within 1 second
        // Verify no callback after cancel
    })
    
    t.Run("reconnection", func(t *testing.T) {
        // Start stream
        // Kill server
        // Verify error logged
        // Restart server
        // Restart stream
        // Verify receives new data
    })
}
```

**Acceptance Criteria**:
- ✅ All 5 account updates received in order
- ✅ No data loss during normal operation
- ✅ Error handling logs errors, doesn't crash
- ✅ Context cancellation stops goroutine within 1s
- ✅ Reconnection after server restart works

#### Test: StartPositionStream

```go
func TestRemoteExchange_StartPositionStream(t *testing.T) {
    t.Run("successful_subscription", func(t *testing.T) {
        // Similar to account stream
        // Verify positions received via callback
    })
    
    t.Run("symbol_filtering", func(t *testing.T) {
        // Subscribe to BTCUSDT positions
        // Server sends BTCUSDT and ETHUSDT positions
        // Verify client receives BOTH (filtering on client side)
        // Or verify server filters (depends on implementation)
    })
    
    t.Run("empty_symbol_receives_all", func(t *testing.T) {
        // Subscribe with empty symbol
        // Server sends positions for multiple symbols
        // Verify all received
    })
}
```

**Acceptance Criteria**:
- ✅ Position updates received via callback
- ✅ Symbol filtering works correctly
- ✅ Empty symbol = all positions

### 4.2 ExchangeServer Streaming Tests

**File**: `market_maker/internal/exchange/server_grpc_test.go`

#### Test: SubscribeAccount RPC

```go
func TestExchangeServer_SubscribeAccount(t *testing.T) {
    t.Run("single_client", func(t *testing.T) {
        // 1. Create mock Exchange implementation
        // 2. Create ExchangeServer wrapping mock
        // 3. Start gRPC server on random port
        // 4. Create gRPC client
        // 5. Call SubscribeAccount
        // 6. Trigger account update in mock (call callback)
        // 7. Verify gRPC client receives message
        // 8. Verify protobuf data matches
        // 9. Close stream
        // 10. Verify server cleans up
    })
    
    t.Run("multiple_concurrent_clients", func(t *testing.T) {
        // Start 10 clients subscribing to account stream
        // Trigger 1 account update in exchange
        // Verify all 10 clients receive the update
        // Verify message broadcast works
    })
    
    t.Run("client_disconnect", func(t *testing.T) {
        // Client subscribes
        // Client closes connection abruptly
        // Verify server doesn't crash
        // Verify server logs disconnect
        // Verify server cleans up goroutines
    })
    
    t.Run("exchange_error", func(t *testing.T) {
        // Mock exchange returns error from StartAccountStream
        // Verify server returns gRPC error to client
        // Verify error code is appropriate
    })
}
```

**Acceptance Criteria**:
- ✅ Single client receives account updates
- ✅ Multiple clients receive broadcasts
- ✅ Server handles client disconnect gracefully
- ✅ Exchange errors propagated to gRPC client

#### Test: SubscribePositions RPC

```go
func TestExchangeServer_SubscribePositions(t *testing.T) {
    t.Run("with_symbol_filter", func(t *testing.T) {
        // Client requests BTCUSDT positions
        // Mock exchange sends BTCUSDT position update
        // Verify client receives it
        // Mock sends ETHUSDT update
        // Verify client does NOT receive it (server filters)
    })
    
    t.Run("without_symbol_filter", func(t *testing.T) {
        // Client requests all positions (empty symbol)
        // Mock sends multiple symbol updates
        // Verify client receives all
    })
}
```

**Acceptance Criteria**:
- ✅ Symbol filtering implemented correctly
- ✅ Empty symbol = all positions forwarded

### 4.3 Mock Exchange Implementation

**File**: `market_maker/internal/exchange/mock/mock_exchange.go`

```go
type MockExchange struct {
    accountCallbacks  []func(*pb.Account)
    positionCallbacks []func(*pb.Position)
    mu                sync.Mutex
}

func (m *MockExchange) StartAccountStream(ctx context.Context, callback func(*pb.Account)) error {
    m.mu.Lock()
    m.accountCallbacks = append(m.accountCallbacks, callback)
    m.mu.Unlock()
    
    <-ctx.Done()
    return ctx.Err()
}

func (m *MockExchange) TriggerAccountUpdate(account *pb.Account) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    for _, cb := range m.accountCallbacks {
        cb(account)
    }
}

// Similar for positions
```

**Purpose**: Controllable exchange for testing server behavior.

---

## 5. End-to-End Tests

### 5.1 Full Stack Test

**File**: `market_maker/tests/integration/grpc_e2e_test.go`

#### Test: Complete gRPC Architecture

```go
func TestE2E_gRPCArchitecture(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping E2E test in short mode")
    }
    
    t.Run("full_stack", func(t *testing.T) {
        // SETUP PHASE
        // 1. Create mock exchange implementation
        // 2. Start exchange_connector binary with mock exchange
        //    - Listen on localhost:50051
        //    - Wait for health check to pass
        
        // 3. Start market_maker binary
        //    - Configure current_exchange: remote
        //    - Configure exchange_grpc_address: localhost:50051
        //    - Wait for startup
        
        // 4. Start live_server binary
        //    - Configure exchange_type: remote
        //    - Configure exchange_grpc_address: localhost:50051
        //    - Listen on localhost:8081
        //    - Wait for startup
        
        // VERIFICATION PHASE
        // 5. Trigger account update in mock exchange
        //    mockExchange.UpdateAccount(&pb.Account{...})
        
        // 6. Verify both market_maker and live_server receive update
        //    - Check market_maker logs for account update
        //    - Query live_server API for account data
        //    - Verify data matches
        
        // 7. Trigger position update in mock exchange
        //    mockExchange.UpdatePosition(&pb.Position{...})
        
        // 8. Verify both clients receive position update
        
        // 9. Verify single exchange connection
        //    - Check mock exchange: connection counter == 1
        //    - This is CRITICAL: proves architecture works
        
        // SHUTDOWN PHASE
        // 10. Send SIGTERM to market_maker
        //     - Verify graceful shutdown (no errors)
        
        // 11. Send SIGTERM to live_server
        //     - Verify graceful shutdown
        
        // 12. Send SIGTERM to exchange_connector
        //     - Verify graceful shutdown
        
        // 13. Verify no goroutine leaks
        //     - Check runtime.NumGoroutine() before/after
        //     - Allow 5 second stabilization period
    })
}
```

**Acceptance Criteria**:
- ✅ All three binaries start successfully
- ✅ Both clients connect to exchange_connector
- ✅ Account update received by both clients
- ✅ Position update received by both clients
- ✅ Data consistency (same data in both clients)
- ✅ **CRITICAL**: Only ONE exchange connection
- ✅ Graceful shutdown (no errors)
- ✅ No goroutine leaks
- ✅ Test completes in <30 seconds

### 5.2 Docker Compose Test

**File**: `market_maker/tests/integration/docker_e2e_test.go`

```go
func TestE2E_DockerCompose(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping Docker E2E test in short mode")
    }
    
    // 1. Run: docker-compose -f docker-compose.grpc.yml up -d
    // 2. Wait for health checks to pass (60s timeout)
    // 3. Query live_server API: GET /api/account
    // 4. Verify returns valid account data
    // 5. Query: GET /api/positions
    // 6. Verify returns positions
    // 7. Run: docker-compose -f docker-compose.grpc.yml down
    // 8. Verify clean shutdown
}
```

**Acceptance Criteria**:
- ✅ Docker Compose stack starts successfully
- ✅ Health checks pass within 60s
- ✅ Live server API returns valid data
- ✅ Clean shutdown without errors

---

## 6. Performance Tests

### 6.1 Latency Benchmarks

**File**: `market_maker/tests/benchmarks/grpc_bench_test.go`

#### Benchmark: GetAccount Latency

```go
func BenchmarkRemoteExchange_GetAccount(b *testing.B) {
    // Setup: Start gRPC server
    server := startTestServer(b)
    defer server.Stop()
    
    client := exchange.NewRemoteExchange("localhost:50051", logger)
    ctx := context.Background()
    
    // Benchmark loop
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, err := client.GetAccount(ctx)
        if err != nil {
            b.Fatal(err)
        }
    }
}
```

**Target**: <2ms per call (p50), <5ms (p99)

#### Benchmark: Streaming Throughput

```go
func BenchmarkRemoteExchange_AccountStreamThroughput(b *testing.B) {
    // Measure messages/second throughput
    // Target: >1000 messages/second
    
    server := startTestServer(b)
    defer server.Stop()
    
    client := exchange.NewRemoteExchange("localhost:50051", logger)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    received := atomic.Int64{}
    
    client.StartAccountStream(ctx, func(account *pb.Account) {
        received.Add(1)
    })
    
    // Server sends updates as fast as possible
    go func() {
        for i := 0; i < b.N; i++ {
            server.TriggerAccountUpdate(testAccount)
        }
    }()
    
    // Wait for all received
    for received.Load() < int64(b.N) {
        time.Sleep(10 * time.Millisecond)
    }
    
    // Report throughput
    b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "msgs/sec")
}
```

**Target**: >1000 messages/second

### 6.2 Memory Profiling

```go
func TestMemoryLeaks(t *testing.T) {
    // 1. Start gRPC server and client
    // 2. Run account stream for 60 seconds
    // 3. Measure memory before and after
    // 4. Verify memory growth <10MB
    
    runtime.GC()
    var before, after runtime.MemStats
    runtime.ReadMemStats(&before)
    
    // Run streaming for 60 seconds
    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()
    
    client.StartAccountStream(ctx, func(account *pb.Account) {
        // Process updates
    })
    
    <-ctx.Done()
    
    runtime.GC()
    runtime.ReadMemStats(&after)
    
    growth := after.Alloc - before.Alloc
    if growth > 10*1024*1024 { // 10MB
        t.Errorf("Memory leak detected: grew %d bytes", growth)
    }
}
```

**Acceptance Criteria**:
- ✅ Memory growth <10MB after 60s streaming
- ✅ No goroutine leaks (stable goroutine count)

### 6.3 Load Testing

**File**: `market_maker/tests/load/grpc_load_test.go`

```go
func TestLoad_MultipleClients(t *testing.T) {
    // Simulate production load:
    // - 1 exchange_connector
    // - 10 market_maker clients
    // - 5 live_server clients
    // - 100 account updates/second
    // - Run for 5 minutes
    
    // Verify:
    // - No dropped messages
    // - Latency <10ms (p99)
    // - Server CPU <80%
    // - Server memory <1GB
}
```

**Target Load**:
- 15 concurrent clients
- 100 updates/second
- 5 minute duration
- 0% message loss
- p99 latency <10ms

---

## 7. Stability Tests

### 7.1 24-Hour Soak Test

**File**: `market_maker/tests/stability/soak_test.go`

```go
func TestSoak_24Hours(t *testing.T) {
    if os.Getenv("SOAK_TEST") != "1" {
        t.Skip("Set SOAK_TEST=1 to run 24h test")
    }
    
    // 1. Start exchange_connector + clients
    // 2. Generate realistic load for 24 hours:
    //    - Account updates every 5 seconds
    //    - Position updates every 1 second
    //    - Random client restarts (simulate deployments)
    // 3. Monitor metrics:
    //    - Memory usage (hourly snapshots)
    //    - Goroutine count
    //    - Error rate
    //    - Latency p50/p99
    // 4. Assert at end:
    //    - Error rate <0.01%
    //    - Memory stable (growth <50MB)
    //    - Goroutines stable (±10)
    //    - No crashes
}
```

**Run Command**:
```bash
SOAK_TEST=1 go test -v -timeout 25h ./tests/stability
```

**Acceptance Criteria**:
- ✅ Runs for full 24 hours without crash
- ✅ Error rate <0.01%
- ✅ Memory stable (<50MB growth)
- ✅ Goroutine count stable (±10)
- ✅ Latency stable (no degradation)

---

## 8. Test Data

### 8.1 Mock Account Data

```go
var testAccount = &pb.Account{
    TotalBalance: "10000.00",
    AvailableBalance: "5000.00",
    Equity: "9500.00",
    UnrealizedPnl: "-500.00",
    MarginUsed: "5000.00",
    MarginLevel: "190.00",
    Leverage: "10.0",
    UpdatedAt: timestamppb.Now(),
}
```

### 8.2 Mock Position Data

```go
var testPositions = []*pb.Position{
    {
        Symbol: "BTCUSDT",
        Side: "LONG",
        Size: "0.5",
        EntryPrice: "40000.00",
        MarkPrice: "41000.00",
        Leverage: "10",
        UnrealizedPnl: "500.00",
        Liquidation: "36000.00",
        MarginType: "ISOLATED",
    },
    {
        Symbol: "ETHUSDT",
        Side: "SHORT",
        Size: "10.0",
        EntryPrice: "3000.00",
        MarkPrice: "2900.00",
        Leverage: "5",
        UnrealizedPnl: "1000.00",
        Liquidation: "3500.00",
        MarginType: "CROSS",
    },
}
```

---

## 9. Test Execution Plan

### Phase 1: Unit Tests (Day 1, 2-3 hours)
1. Write RemoteExchange unit tests
2. Write ExchangeServer unit tests
3. Run tests: `go test ./internal/exchange -v`
4. Fix any failures
5. Achieve 80% coverage

### Phase 2: Integration Tests (Day 1-2, 4-6 hours)
1. Implement mock exchange
2. Write RemoteExchange streaming tests
3. Write ExchangeServer RPC tests
4. Run tests: `go test ./internal/exchange -v -run Integration`
5. Fix any failures
6. Achieve 70% coverage

### Phase 3: E2E Tests (Day 2, 3-4 hours)
1. Write full stack E2E test
2. Write Docker Compose test
3. Run tests: `go test ./tests/integration -v`
4. Fix any failures
5. Verify architecture compliance (single connection)

### Phase 4: Performance Tests (Day 3, 2-3 hours)
1. Write latency benchmarks
2. Write throughput benchmarks
3. Write memory leak tests
4. Run benchmarks: `go test ./tests/benchmarks -bench=. -benchmem`
5. Verify targets met
6. Document results

### Phase 5: Stability Tests (Day 3-4, 24+ hours)
1. Write soak test
2. Run soak test: `SOAK_TEST=1 go test -timeout 25h ./tests/stability`
3. Monitor metrics
4. Analyze results
5. Fix any issues found

---

## 10. Success Criteria

| Criterion | Target | Status |
|-----------|--------|--------|
| Unit tests pass | 100% | ⏳ TODO |
| Integration tests pass | 100% | ⏳ TODO |
| E2E test passes | 100% | ⏳ TODO |
| Code coverage | >80% | ⏳ TODO |
| GetAccount latency (p50) | <2ms | ⏳ TODO |
| GetAccount latency (p99) | <5ms | ⏳ TODO |
| Stream throughput | >1000 msg/s | ⏳ TODO |
| Memory leak (60s) | <10MB | ⏳ TODO |
| 24h soak test | Pass | ⏳ TODO |
| Single exchange connection | Verified | ⏳ TODO |

---

## 11. Test Automation

### CI/CD Integration

```yaml
# .github/workflows/phase16-tests.yml
name: Phase 16 gRPC Tests

on:
  push:
    branches: [main, phase16/*]
  pull_request:
    branches: [main]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      - run: cd market_maker && go test ./internal/exchange -v -race
      
  integration-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      - run: cd market_maker && go test ./internal/exchange -v -race -run Integration
      
  e2e-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      - run: cd market_maker && go test ./tests/integration -v -timeout 5m
      
  benchmarks:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      - run: cd market_maker && go test ./tests/benchmarks -bench=. -benchmem
```

### Local Test Scripts

**File**: `market_maker/scripts/test_grpc.sh`

```bash
#!/bin/bash
set -e

echo "Running Phase 16 gRPC Tests..."

# Unit tests
echo "1/4 Running unit tests..."
go test ./internal/exchange -v -race -count=1

# Integration tests
echo "2/4 Running integration tests..."
go test ./internal/exchange -v -race -run Integration -count=1

# E2E tests
echo "3/4 Running E2E tests..."
go test ./tests/integration -v -timeout 5m -count=1

# Benchmarks
echo "4/4 Running benchmarks..."
go test ./tests/benchmarks -bench=. -benchmem -count=1

echo "✅ All tests passed!"
```

---

## 12. Troubleshooting Test Failures

### Common Issues

**Issue**: "dial tcp: connection refused"
- **Cause**: gRPC server not started or wrong port
- **Fix**: Check server startup, verify port not in use

**Issue**: "test timeout after 10m"
- **Cause**: Deadlock or goroutine leak
- **Fix**: Add `-timeout 30s` to individual tests, check for missing context cancellation

**Issue**: "too many open files"
- **Cause**: Not closing gRPC connections
- **Fix**: Add `defer client.Close()` in all tests

**Issue**: "panic: send on closed channel"
- **Cause**: Race condition in stream handling
- **Fix**: Use proper locking, check stream context before sending

---

## 13. Next Steps

After all tests pass:

1. **Update Documentation**
   - Add test results to requirements.md
   - Update plan.md with completion status
   - Create deployment guide in README.md

2. **Create Migration Guide**
   - Document steps to migrate from native to gRPC
   - Include rollback procedure
   - Add troubleshooting section

3. **Performance Baseline**
   - Document benchmark results
   - Create performance monitoring dashboard
   - Set up alerts for regression

4. **Production Readiness**
   - Security audit
   - Load testing report
   - Disaster recovery plan
   - Operational runbook

---

## Document Change Log

| Date | Version | Changes |
|------|---------|---------|
| 2026-01-22 | 1.0 | Initial test specification created |
