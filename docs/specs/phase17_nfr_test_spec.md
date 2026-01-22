# Phase 17: Non-Functional Requirements (NFR) Test Specification

**Project**: OpenSQT Market Maker  
**Phase**: 17 - Quality Assurance & NFR Validation  
**Version**: 1.0  
**Date**: January 22, 2026  
**Status**: Active  
**Approach**: Specs-Driven Development + Test-Driven Development (TDD)

---

## Table of Contents

1. [Overview](#1-overview)
2. [Test Strategy](#2-test-strategy)
3. [Integration Tests](#3-integration-tests)
4. [End-to-End Tests](#4-end-to-end-tests)
5. [Performance Benchmarks](#5-performance-benchmarks)
6. [Stability Tests](#6-stability-tests)
7. [Acceptance Criteria](#7-acceptance-criteria)
8. [Test Environment](#8-test-environment)

---

## 1. Overview

### 1.1 Purpose

This document specifies the Non-Functional Requirements (NFRs) testing strategy for the gRPC-based architecture implemented in Phase 16. All Functional Requirements (FRs) are complete (100% compliance); this phase validates quality attributes.

### 1.2 Scope

**In Scope**:
- Integration testing (gRPC client/server)
- End-to-end testing (full stack)
- Performance benchmarking (latency, throughput)
- Stability testing (24-hour soak, memory leaks)
- Production readiness assessment

**Out of Scope**:
- Unit tests (already covered in Phase 16)
- Frontend testing (covered in Phase 15)
- Security penetration testing (future phase)
- Load testing beyond 1000 msg/s (future phase)

### 1.3 Test Priorities

| Priority | Test Type | Rationale |
|----------|-----------|-----------|
| 🔴 HIGH | Integration Tests | Critical for gRPC communication |
| 🔴 HIGH | E2E Full Stack | Validates production deployment |
| 🟡 MEDIUM | Performance Benchmarks | Validates latency requirements |
| 🟡 MEDIUM | Failure Recovery | Validates retry logic |
| 🟢 LOW | 24-Hour Stability | Validates long-term reliability |

### 1.4 Success Criteria

Phase 17 is complete when:
- ✅ All integration tests pass
- ✅ E2E full stack test passes
- ✅ Performance benchmarks meet targets
- ✅ No memory leaks detected
- ✅ Production readiness report approved

---

## 2. Test Strategy

### 2.1 Test-Driven Development (TDD) Approach

For each test suite, follow this workflow:

```
1. SPEC: Write test specification
   - Define acceptance criteria
   - Document expected behavior
   - Create test matrix

2. RED: Write failing test
   - Implement test code
   - Run test → MUST FAIL
   - Commit: "test: add [test name]"

3. GREEN: Make test pass
   - Fix issues found
   - Run test → MUST PASS
   - Commit: "fix: [issue description]"

4. REFACTOR: Improve quality
   - Optimize code
   - Run test → MUST STILL PASS
   - Commit: "refactor: [improvement]"

5. DOCUMENT: Update tracking
   - Mark test as complete in plan.md
   - Update requirements.md NFR status
```

### 2.2 Test Environment Setup

**Prerequisites**:
```bash
# Build all binaries
cd market_maker
go build ./cmd/...

# Ensure grpc_health_probe is available
./bin/grpc_health_probe --version

# Verify Docker Compose
docker-compose -f docker-compose.grpc.yml config
```

**Test Data**:
- Use mock exchange for integration tests
- Use testnet for E2E tests (if available)
- Generate synthetic load for performance tests

### 2.3 Test Metrics Tracking

For each test, collect:
- **Pass/Fail Status**
- **Execution Time**
- **Resource Usage** (CPU, memory)
- **Error Messages** (if failed)
- **Retry Count** (for flaky tests)

---

## 3. Integration Tests

### 3.1 RemoteExchange Integration Tests

**File**: `tests/integration/remote_integration_test.go`

**Objective**: Validate gRPC client (RemoteExchange) interacts correctly with ExchangeServer

#### Test 3.1.1: Account Stream Subscription

**Specification**:
```
GIVEN: exchange_connector is running with mock exchange
WHEN: RemoteExchange calls StartAccountStream()
THEN: Account updates are received via callback
AND: Stream stays open until context cancelled
AND: No goroutines leak after cancellation
```

**Acceptance Criteria**:
- ✅ Callback receives at least 1 account update within 10 seconds
- ✅ Account data matches expected structure (balances, margin)
- ✅ Context cancellation stops stream within 1 second
- ✅ Goroutine count returns to baseline after cleanup

**Test Code Structure**:
```go
func TestRemoteExchange_AccountStream_Integration(t *testing.T) {
    // Setup: Start exchange_connector with mock
    // Action: Subscribe to account stream
    // Assert: Receive updates, clean shutdown
    // Cleanup: Verify no leaks
}
```

**Estimated Effort**: 1 hour

---

#### Test 3.1.2: Position Stream Subscription

**Specification**:
```
GIVEN: exchange_connector is running
WHEN: RemoteExchange calls StartPositionStream() with symbol filter
THEN: Only matching position updates are received
AND: Stream handles multiple concurrent subscriptions
```

**Acceptance Criteria**:
- ✅ Symbol filtering works correctly (BTCUSDT only)
- ✅ Multiple clients can subscribe independently
- ✅ Updates are not duplicated across clients

**Estimated Effort**: 1 hour

---

#### Test 3.1.3: Reconnection After Server Restart

**Specification**:
```
GIVEN: RemoteExchange connected to exchange_connector
WHEN: exchange_connector is killed and restarted
THEN: RemoteExchange automatically reconnects within 10 seconds
AND: Streams resume without data loss
```

**Acceptance Criteria**:
- ✅ Retry logic triggers on disconnect
- ✅ Connection re-established within 10 retries
- ✅ No duplicate subscriptions after reconnect

**Estimated Effort**: 1.5 hours

---

#### Test 3.1.4: Concurrent Stream Subscriptions

**Specification**:
```
GIVEN: Multiple RemoteExchange clients
WHEN: All clients subscribe to same streams simultaneously
THEN: Server handles concurrent subscriptions without errors
AND: Each client receives independent updates
```

**Acceptance Criteria**:
- ✅ 5 concurrent clients supported
- ✅ No race conditions detected
- ✅ Message delivery is independent per client

**Estimated Effort**: 1 hour

---

#### Test 3.1.5: Stream Error Handling

**Specification**:
```
GIVEN: RemoteExchange subscribed to stream
WHEN: Server sends error on stream
THEN: Client receives error via callback
AND: Stream is properly cleaned up
```

**Acceptance Criteria**:
- ✅ Error propagates to client
- ✅ Client can resubscribe after error
- ✅ No panic on error

**Estimated Effort**: 0.5 hours

---

### 3.2 ExchangeServer Integration Tests

**File**: `tests/integration/server_integration_test.go`

**Objective**: Validate gRPC server (ExchangeServer) correctly wraps native connectors

#### Test 3.2.1: Multi-Client Broadcast

**Specification**:
```
GIVEN: ExchangeServer with 3 connected clients
WHEN: Native connector generates position update
THEN: All 3 clients receive the same update
AND: No client blocks other clients
```

**Acceptance Criteria**:
- ✅ Broadcast to 3+ clients works
- ✅ Slow client doesn't block fast clients
- ✅ All clients receive identical data

**Estimated Effort**: 1.5 hours

---

#### Test 3.2.2: Client Disconnect Cleanup

**Specification**:
```
GIVEN: Client connected with active stream
WHEN: Client disconnects abruptly (no graceful close)
THEN: Server detects disconnect within 5 seconds
AND: Server cleans up resources (goroutines, channels)
```

**Acceptance Criteria**:
- ✅ Disconnect detected via context cancellation
- ✅ Goroutine count decreases after disconnect
- ✅ No resource leaks

**Estimated Effort**: 1 hour

---

#### Test 3.2.3: Health Check Integration

**Specification**:
```
GIVEN: ExchangeServer running
WHEN: grpc_health_probe queries health
THEN: Returns SERVING status
AND: Health reflects actual service state
```

**Acceptance Criteria**:
- ✅ Health probe returns SERVING
- ✅ Health check works for both overall and service-specific
- ✅ Health changes to NOT_SERVING if exchange fails

**Estimated Effort**: 0.5 hours

---

#### Test 3.2.4: Credential Validation Integration

**Specification**:
```
GIVEN: ExchangeServer starting with invalid credentials
WHEN: Startup validation runs
THEN: Server exits with error code 1
AND: Error message clearly states credential failure
```

**Acceptance Criteria**:
- ✅ Invalid credentials → exit code 1
- ✅ Error message contains "credential validation failed"
- ✅ Server does NOT start serving on validation failure

**Estimated Effort**: 0.5 hours

---

#### Test 3.2.5: All Exchange Backends

**Specification**:
```
GIVEN: ExchangeServer configured for each exchange (binance, bitget, gate, okx, bybit)
WHEN: Server starts and handles requests
THEN: All exchanges work correctly via gRPC
```

**Acceptance Criteria**:
- ✅ Binance backend works
- ✅ Bitget backend works
- ✅ Gate backend works
- ✅ OKX backend works
- ✅ Bybit backend works

**Estimated Effort**: 2 hours (0.4h per exchange)

---

**Total Integration Test Effort**: 10.5 hours

---

## 4. End-to-End Tests

### 4.1 Full Stack Deployment Test

**File**: `tests/e2e/grpc_stack_test.go`

**Objective**: Validate complete gRPC architecture with all services running

#### Test 4.1.1: Full Stack Startup

**Specification**:
```
GIVEN: Docker Compose with all services (exchange_connector, market_maker, live_server)
WHEN: Stack is started with docker-compose up
THEN: All services start successfully
AND: Health checks pass for all services
AND: Dependencies are respected (connector starts first)
```

**Acceptance Criteria**:
- ✅ exchange_connector health check: SERVING within 10s
- ✅ market_maker connects within 30s
- ✅ live_server connects within 30s
- ✅ No error logs during startup

**Test Steps**:
1. Clean environment: `docker-compose down -v`
2. Start stack: `docker-compose -f docker-compose.grpc.yml up -d`
3. Wait for health checks
4. Query health endpoints
5. Verify logs for successful connection

**Estimated Effort**: 1.5 hours

---

#### Test 4.1.2: Single Exchange Connection

**Specification**:
```
GIVEN: Full stack running
WHEN: Monitoring exchange WebSocket connections
THEN: Only ONE connection exists to exchange
AND: Both market_maker and live_server share this connection
```

**Acceptance Criteria**:
- ✅ Exchange sees exactly 1 WebSocket connection
- ✅ market_maker receives data
- ✅ live_server receives data
- ✅ Data is consistent across both binaries

**Critical**: This validates the PRIMARY architectural requirement

**Estimated Effort**: 2 hours

---

#### Test 4.1.3: Trade Execution Through Stack

**Specification**:
```
GIVEN: Full stack running with testnet
WHEN: market_maker places order
THEN: Order is executed via exchange_connector
AND: live_server receives order update
AND: Data is consistent in both binaries
```

**Acceptance Criteria**:
- ✅ Order placement succeeds
- ✅ Order appears in exchange
- ✅ Both binaries see same order ID
- ✅ Latency \u003c 100ms end-to-end

**Estimated Effort**: 2 hours

---

### 4.2 Failure Recovery Test

**File**: `tests/e2e/failure_recovery_test.go`

**Objective**: Validate retry and recovery behavior

#### Test 4.2.1: Exchange Connector Restart

**Specification**:
```
GIVEN: Full stack running with active trading
WHEN: exchange_connector is killed (SIGKILL)
AND: exchange_connector is restarted within 30 seconds
THEN: market_maker retries and reconnects automatically
AND: live_server retries and reconnects automatically
AND: Streams resume without manual intervention
```

**Acceptance Criteria**:
- ✅ Retry logic triggers on disconnect
- ✅ Both clients reconnect within 10 retries
- ✅ No data loss during restart
- ✅ Streams continue after reconnect

**Estimated Effort**: 2 hours

---

#### Test 4.2.2: Prolonged Connector Downtime

**Specification**:
```
GIVEN: Full stack running
WHEN: exchange_connector is killed and NOT restarted
THEN: market_maker exits after 10 retries (~5 minutes)
AND: live_server exits after 10 retries
AND: Error messages clearly state failure reason
```

**Acceptance Criteria**:
- ✅ Fail-fast after 10 retries
- ✅ Exit code 1
- ✅ Error: "failed to connect after 10 attempts"

**Estimated Effort**: 1 hour

---

### 4.3 Graceful Shutdown Test

**File**: `tests/e2e/shutdown_test.go`

**Objective**: Validate clean shutdown behavior

#### Test 4.3.1: Ordered Shutdown

**Specification**:
```
GIVEN: Full stack running
WHEN: SIGTERM sent to all services
THEN: Clients shut down first (market_maker, live_server)
AND: Server shuts down last (exchange_connector)
AND: No orphaned goroutines
AND: No data loss
```

**Acceptance Criteria**:
- ✅ Shutdown completes within 10 seconds
- ✅ No panic during shutdown
- ✅ Logs show "graceful shutdown" messages
- ✅ Docker Compose down succeeds

**Estimated Effort**: 1.5 hours

---

**Total E2E Test Effort**: 10 hours

---

## 5. Performance Benchmarks

### 5.1 Latency Benchmarks

**File**: `tests/benchmarks/latency_bench_test.go`

**Objective**: Measure gRPC call latencies

#### Benchmark 5.1.1: GetAccount Latency

**Specification**:
```
GIVEN: RemoteExchange connected to exchange_connector
WHEN: GetAccount() is called 1000 times
THEN: Measure p50, p95, p99 latencies
```

**Targets**:
- p50: \u003c 2ms
- p95: \u003c 4ms
- p99: \u003c 5ms

**Benchmark Code**:
```go
func BenchmarkGetAccount_Latency(b *testing.B) {
    // Setup: Connect RemoteExchange
    for i := 0; i \u003c b.N; i++ {
        start := time.Now()
        _, err := remote.GetAccount(ctx)
        latency := time.Since(start)
        // Record latency
    }
    // Calculate percentiles
}
```

**Estimated Effort**: 1 hour

---

#### Benchmark 5.1.2: PlaceOrder Latency

**Specification**:
```
GIVEN: RemoteExchange connected
WHEN: PlaceOrder() is called 1000 times
THEN: Measure latencies
```

**Targets**:
- p50: \u003c 3ms
- p95: \u003c 8ms
- p99: \u003c 10ms

**Estimated Effort**: 1 hour

---

#### Benchmark 5.1.3: Stream Message Latency

**Specification**:
```
GIVEN: Active price stream
WHEN: Server sends message
THEN: Measure time until client receives
```

**Targets**:
- p50: \u003c 1ms
- p95: \u003c 2ms
- p99: \u003c 3ms

**Estimated Effort**: 1.5 hours

---

### 5.2 Throughput Benchmarks

**File**: `tests/benchmarks/throughput_bench_test.go`

#### Benchmark 5.2.1: Order Placement Throughput

**Specification**:
```
GIVEN: RemoteExchange connected
WHEN: Placing orders as fast as possible
THEN: Measure orders per second
```

**Target**: \u003e 1000 orders/second

**Estimated Effort**: 1 hour

---

#### Benchmark 5.2.2: Stream Message Throughput

**Specification**:
```
GIVEN: Active stream subscription
WHEN: Server sends messages at maximum rate
THEN: Measure messages per second
```

**Target**: \u003e 5000 messages/second

**Estimated Effort**: 1 hour

---

#### Benchmark 5.2.3: Concurrent Client Capacity

**Specification**:
```
GIVEN: Exchange_connector running
WHEN: Connecting 10+ concurrent clients
THEN: Measure performance degradation
```

**Target**: No degradation with \u003c 10 clients

**Estimated Effort**: 1.5 hours

---

### 5.3 Comparison Benchmarks

**File**: `tests/benchmarks/comparison_bench_test.go`

#### Benchmark 5.3.1: Native vs gRPC Latency

**Specification**:
```
GIVEN: Same operation via native and gRPC
WHEN: Measuring latency for both
THEN: Compare overhead
```

**Target**: gRPC overhead \u003c 2ms

**Estimated Effort**: 1 hour

---

**Total Performance Benchmark Effort**: 9 hours

---

## 6. Stability Tests

### 6.1 24-Hour Soak Test

**File**: `tests/stability/soak_test.go`

**Objective**: Validate long-term stability

#### Test 6.1.1: Continuous Operation

**Specification**:
```
GIVEN: Full stack deployed
WHEN: Running continuous trading for 24 hours
THEN: No crashes occur
AND: Memory growth \u003c 10MB
AND: Goroutine count remains stable
AND: Error rate \u003c 0.01%
```

**Metrics to Collect**:
- Memory usage (every 1 minute)
- Goroutine count (every 1 minute)
- Error count
- Request count
- Success rate

**Acceptance Criteria**:
- ✅ 24 hours uptime
- ✅ Memory growth \u003c 10MB
- ✅ Goroutine count ± 10 from baseline
- ✅ Error rate \u003c 0.01%

**Test Script**:
```bash
#!/bin/bash
# scripts/run_soak_test.sh
docker-compose -f docker-compose.grpc.yml up -d
for i in {1..1440}; do  # 1440 minutes = 24 hours
    # Collect metrics
    docker stats --no-stream >> soak_metrics.log
    sleep 60
done
```

**Estimated Effort**: 2 hours setup + 24 hours runtime + 2 hours analysis = 28 hours

---

### 6.2 Memory Leak Detection

**File**: `tests/stability/leak_test.go`

**Objective**: Detect memory leaks

#### Test 6.2.1: Stream Subscription Leaks

**Specification**:
```
GIVEN: RemoteExchange
WHEN: Subscribing and unsubscribing 1000 times
THEN: Memory returns to baseline
AND: Goroutine count returns to baseline
```

**Acceptance Criteria**:
- ✅ Heap growth \u003c 5MB after 1000 cycles
- ✅ Goroutine count returns to ±5 of baseline

**Tool**: Use `pprof` for heap analysis

**Estimated Effort**: 2 hours

---

### 6.3 Stress Testing

**File**: `tests/stability/stress_test.go`

#### Test 6.3.1: High Order Rate

**Specification**:
```
GIVEN: exchange_connector running
WHEN: Sending 1000 orders/second for 1 minute
THEN: System handles load without errors
```

**Estimated Effort**: 1.5 hours

---

#### Test 6.3.2: Rapid Connect/Disconnect

**Specification**:
```
GIVEN: exchange_connector running
WHEN: Clients connect and disconnect rapidly (100 cycles)
THEN: No goroutine leaks
AND: No connection pool exhaustion
```

**Estimated Effort**: 1.5 hours

---

**Total Stability Test Effort**: 35 hours (including 24h soak time)

---

## 7. Acceptance Criteria

### 7.1 Test Coverage

| Test Suite | Tests | Status | Required Pass Rate |
|------------|-------|--------|-------------------|
| Integration | 10 | Pending | 100% |
| E2E | 6 | Pending | 100% |
| Performance | 9 | Pending | 80% (targets met) |
| Stability | 4 | Pending | 100% |
| **TOTAL** | **29** | **Pending** | **95%** |

### 7.2 Performance Targets

| Metric | Target | Acceptance |
|--------|--------|------------|
| GetAccount p99 | \u003c 5ms | Required |
| PlaceOrder p99 | \u003c 10ms | Required |
| Stream latency p99 | \u003c 3ms | Required |
| Order throughput | \u003e 1000/s | Required |
| Stream throughput | \u003e 5000/s | Recommended |
| Concurrent clients | 10 | Required |
| gRPC overhead | \u003c 2ms | Recommended |

### 7.3 Stability Targets

| Metric | Target | Acceptance |
|--------|--------|------------|
| 24h uptime | 100% | Required |
| Memory growth | \u003c 10MB | Required |
| Goroutine stability | ±10 | Required |
| Error rate | \u003c 0.01% | Required |
| Recovery time | \u003c 1 min | Required |

---

## 8. Test Environment

### 8.1 Hardware Requirements

- **CPU**: 4+ cores
- **RAM**: 8GB minimum
- **Disk**: 20GB free space
- **Network**: Low latency (\u003c 10ms to exchange)

### 8.2 Software Requirements

- **Go**: 1.21+
- **Docker**: 20.10+
- **Docker Compose**: 2.0+
- **grpc_health_probe**: 0.4.24
- **pprof**: Built into Go

### 8.3 Test Data

- **Mock Exchange**: For integration tests
- **Testnet**: For E2E tests (if available)
- **Synthetic Load**: Generated programmatically

---

## 9. Execution Plan

### 9.1 Week 1: Integration \u0026 E2E

**Day 1-2**: Integration tests (10.5 hours)
- RemoteExchange tests
- ExchangeServer tests

**Day 3-4**: E2E tests (10 hours)
- Full stack deployment
- Failure recovery
- Graceful shutdown

**Day 5**: Review and fixes (8 hours)

### 9.2 Week 2: Performance \u0026 Stability

**Day 1-2**: Performance benchmarks (9 hours)
- Latency benchmarks
- Throughput benchmarks
- Comparison benchmarks

**Day 3**: Stability tests setup (5 hours)

**Day 4-5**: 24-hour soak test (runtime)

**Day 5**: Analysis and documentation (8 hours)

### Total Estimated Effort: 50.5 hours + 24h soak runtime

---

## 10. Reporting

### 10.1 Test Reports

For each test suite, generate:
- Pass/fail summary
- Performance metrics
- Error analysis
- Recommendations

### 10.2 Final Production Readiness Report

Document will include:
- FR compliance: 100% ✅
- NFR compliance: TBD
- Known limitations
- Deployment recommendations
- Monitoring recommendations

---

**Specification Version**: 1.0  
**Status**: APPROVED  
**Next Step**: Begin implementation with TDD (Phase 17.2.1)
