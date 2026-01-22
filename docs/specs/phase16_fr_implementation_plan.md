# Phase 16.9: Critical Functional Requirements Implementation

**Version**: 1.0  
**Date**: January 22, 2026  
**Status**: ACTIVE  
**Approach**: Specs-Driven Development + TDD

---

## Executive Summary

After completing Phase 16.8 architecture review, we identified **5 critical functional requirements (FRs)** that are missing from the gRPC implementation. This phase focuses on implementing these FRs using Test-Driven Development.

**Key Principle**: **FRs over NFRs** - Functional completeness is prioritized over quality validation.

---

## FR Implementation Sequence

### 1. FR-16.9.1: Health Checks (2 hours) - HIGHEST PRIORITY

**Requirement**: REQ-GRPC-001.4 - Implement `grpc.health.v1.Health` service

**Why First**: Critical for Docker Compose orchestration and production monitoring

**TDD Approach**:
```go
// Step 1: Write test (RED)
func TestExchangeConnector_HealthCheck(t *testing.T) {
    // Test that health check returns SERVING when connector is running
}

// Step 2: Implement (GREEN)
// Implement health check service

// Step 3: Refactor
// Clean up implementation
```

**Deliverables**:
- Health check service registered
- `grpc_health_probe` works
- Docker Compose uses health checks

---

### 2. FR-16.9.2: Credential Validation (3 hours)

**Requirement**: REQ-GRPC-002.3 - Validate credentials before serving

**Why Second**: Prevents late failures, improves startup time

**TDD Approach**:
```go
// Step 1: Write test (RED)
func TestExchangeConnector_InvalidCredentials(t *testing.T) {
    // Test that connector exits with error on invalid credentials
}

// Step 2: Implement (GREEN)
// Add ValidateCredentials() method

// Step 3: Refactor
// Extract validation logic
```

**Deliverables**:
- Credential validation for all 5 exchanges
- Clear error messages
- Fail-fast on invalid credentials

---

### 3. FR-16.9.3: Connection Retry with Backoff (4 hours)

**Requirement**: REQ-GRPC-010.3 - Implement exponential backoff retry

**Why Third**: Improves resilience to temporary failures

**TDD Approach**:
```go
// Step 1: Write test (RED)
func TestRemoteExchange_ConnectionRetry(t *testing.T) {
    // Test that client retries with exponential backoff
}

// Step 2: Implement (GREEN)
// Implement retry logic with backoff

// Step 3: Refactor
// Extract retry logic to reusable function
```

**Deliverables**:
- Retry logic with exponential backoff (1s, 2s, 4s, 8s, max 60s)
- Max 10 retries
- Applied to both market_maker and live_server

---

### 4. FR-16.9.4: Fail-Fast Behavior (2 hours)

**Requirement**: REQ-GRPC-010.4 - Exit if connector unavailable

**Why Fourth**: Prevents hanging processes in production

**TDD Approach**:
```go
// Step 1: Write test (RED)
func TestMarketMaker_FailFast(t *testing.T) {
    // Test that binary exits if connector unavailable
}

// Step 2: Implement (GREEN)
// Add timeout and exit logic

// Step 3: Refactor
// Clean up error handling
```

**Deliverables**:
- 30-second connection timeout
- Exit code 1 on failure
- Clear error messages

---

### 5. FR-16.9.5: Stream Buffering Verification (3 hours)

**Requirement**: REQ-GRPC-004.3 - Buffer messages to prevent blocking

**Why Last**: Less critical, may already be implemented

**TDD Approach**:
```go
// Step 1: Write test (RED)
func TestExchangeServer_StreamBuffering(t *testing.T) {
    // Test that slow client doesn't block server
}

// Step 2: Verify/Implement (GREEN)
// Add buffered channels if needed

// Step 3: Refactor
// Optimize buffer sizes
```

**Deliverables**:
- Buffered channels for all streams
- Client disconnect detection
- No goroutine leaks

---

## Implementation Timeline

| FR | Estimated | Type | Day |
|----|-----------|------|-----|
| FR-16.9.1 Health Checks | 2 hours | Critical | Day 1 AM |
| FR-16.9.2 Credential Validation | 3 hours | Critical | Day 1 PM |
| FR-16.9.3 Connection Retry | 4 hours | Critical | Day 2 AM |
| FR-16.9.4 Fail-Fast | 2 hours | Critical | Day 2 PM |
| FR-16.9.5 Stream Buffering | 3 hours | Medium | Day 2 PM |
| **TOTAL** | **14 hours** | | **2 days** |

---

## TDD Workflow

For each FR, follow this strict workflow:

### Step 1: Write Specification
- Document requirement in detail
- Define acceptance criteria
- Identify test scenarios

### Step 2: Write Failing Test (RED)
- Write test that validates the requirement
- Run test → should FAIL
- Commit: "test: add test for [requirement]"

### Step 3: Implement Minimum Code (GREEN)
- Write minimal code to make test pass
- Run test → should PASS
- Commit: "feat: implement [requirement]"

### Step 4: Refactor
- Clean up implementation
- Improve code quality
- Run test → still PASS
- Commit: "refactor: improve [requirement]"

### Step 5: Document
- Update requirements.md
- Update plan.md
- Update architecture.md if needed

---

## Success Criteria

**Phase 16.9 is complete when**:

- [ ] All 5 FRs implemented
- [ ] All tests pass
- [ ] All binaries compile
- [ ] Docker Compose works with health checks
- [ ] Credential validation works for all exchanges
- [ ] Connection retry demonstrated
- [ ] Fail-fast behavior verified
- [ ] No goroutine leaks
- [ ] Documentation updated
- [ ] FR compliance = 100%

---

## Post-Phase 16.9

**Next Phase**: Phase 17 - Quality Assurance (NFRs)

Tasks deferred to Phase 17:
- Integration test suite
- E2E testing
- Performance benchmarks
- 24-hour stability test
- Load testing
- Documentation of results

**Estimated**: 1 week

---

## References

- [`grpc_architecture_requirements.md`](./grpc_architecture_requirements.md) - Detailed requirements
- [`plan.md`](./plan.md) - Phase tracking
- [`exchange_architecture.md`](./exchange_architecture.md) - Architecture design
