# OpenSQT Market Maker - Requirements & Specs Plan

## Overview

This document serves as the central tracking document for the OpenSQT market maker system replication project. It consolidates requirements, specifications, and design docs extracted from the existing codebase and documentation, following a specs-driven development flow with TDD approach.

**Original System**: High-frequency cryptocurrency market maker for perpetual futures contracts
**Architecture Style**: Modular monolith with clean interfaces transitioning to decoupled gRPC-based connectors
**Target**: Replication with improved architecture, comprehensive testing, and enhanced safety

## 1. Project Requirements

### 1.1 Functional Requirements

#### Core Trading Functionality
- **REQ-TRADE-001**: Support multi-exchange trading (Binance, Bitget, Gate.io, OKX, Bybit)
  - Must maintain unified interface across exchanges
  - Handle exchange-specific API differences (precision, endpoints, authentication)
  - Support both Spot and Futures markets

- **REQ-TRADE-002**: Implement fixed-amount grid trading strategy
  - Each trade invests fixed USDT amount vs traditional fixed quantity
  - Support configurable price intervals and window sizes
  - Maintain buy/sell windows around current market price

- **REQ-TRADE-003**: Real-time price monitoring and trading
  - Single global price source via WebSocket (no REST polling)
  - Millisecond-level response times
  - Atomic price storage for thread-safe access

- **REQ-TRADE-004**: Intelligent position management (Super Slot System)
  - Slot-based lifecycle management (FREE → PENDING → LOCKED → FREE)
  - Prevent concurrent duplicate orders
  - Automatic order lifecycle handling (buy → hold → sell → repeat)

#### Risk Management & Safety
- **REQ-SAFE-001**: Multi-layer risk control system
  - Startup safety checks (balance, leverage, position limits)
  - Active risk monitoring (K-line volume anomaly detection)
  - Position reconciliation (local vs exchange state sync)
  - Automatic order cleanup for stuck orders

- **REQ-SAFE-002**: Graceful system operation
  - Automatic margin lock during insufficient funds
  - Post-only order placement with fallback to limit orders
  - Rate limiting (25 orders/second with burst capacity)
  - Order retry logic with exponential backoff

#### Resilience & Fault Tolerance
- **REQ-RES-001**: Standardized resilience patterns
  - Use `failsafe-go` for retries, circuit breakers, and rate limiters
  - Unified policy management across all exchange adapters
- **REQ-RES-002**: HTTP Client Resilience
  - Configurable retry policies for 5xx errors and network timeouts
  - Circuit breaking for persistent exchange failures
- **REQ-RES-003**: Rate Limit Compliance
  - Respect global and endpoint-specific rate limits
  - Graceful backoff on 429 Too Many Requests responses

### 1.2 Non-Functional Requirements

#### Performance
- **REQ-PERF-001**: Sub-millisecond order execution latency (internal)
- **REQ-PERF-002**: Handle 25+ orders per second with rate limiting
- **REQ-PERF-003**: Support concurrent trading across multiple slots
- **REQ-PERF-004**: Efficient memory usage (no unbounded slot creation)

#### Reliability
- **REQ-REL-001**: 99.9% uptime with automatic WebSocket reconnection
- **REQ-REL-002**: Comprehensive error handling and recovery
- **REQ-REL-003**: Data consistency between local state and exchange
- **REQ-REL-004**: Graceful shutdown with order cancellation
- **REQ-REL-005**: Durable execution of trading workflows (DBOS)

#### Security
- **REQ-SEC-001**: Secure API key management (no hardcoding)
- **REQ-SEC-002**: Input validation for all configuration parameters
- **REQ-SEC-003**: Protection against race conditions in concurrent operations
- **REQ-SEC-004**: Audit logging for all trading operations

#### Maintainability
- **REQ-MAINT-001**: Modular architecture with clear separation of concerns
- **REQ-MAINT-002**: Comprehensive test coverage (unit, integration, end-to-end)
- **REQ-MAINT-003**: Clean interfaces with dependency injection
- **REQ-MAINT-004**: Detailed logging and monitoring capabilities (OTel)
- **REQ-MAINT-005**: Protobuf/gRPC based exchange connectors managed with `buf`

## 2. System Architecture Specifications

### 2.1 Architectural Principles

#### Design Patterns
- **PATTERN-001**: Interface abstraction for exchange adapters
- **PATTERN-002**: gRPC Proxy pattern for remote exchange connectors
- **PATTERN-003**: Observer pattern for WebSocket event handling
- **PATTERN-004**: State machine pattern for slot lifecycle management
- **PATTERN-005**: Durable execution pattern via DBOS

#### Core Components
- **COMP-001**: Exchange Layer (IExchange interface with Remote and Local implementations)
- **COMP-002**: Price Monitor (global WebSocket price source)
- **COMP-003**: Super Position Manager (deterministic slot-based trading logic)
- **COMP-004**: Safety & Risk Control (4-layer protection system)
- **COMP-005**: Order Executor (rate limiting and retry logic)
- **COMP-006**: Workflow Engine (DBOS-powered durable execution)

## 6. Development Phases

### Phase 1: Foundation ✅ COMPLETED
### Phase 2: Core Components ✅ COMPLETED
### Phase 3: Risk Management System ✅ COMPLETED
### Phase 4: System Integration & Real Adapters ✅ COMPLETED
### Phase 5: Production Readiness ✅ COMPLETED
### Phase 6: Observability & OTel Integration ✅ COMPLETED
### Phase 7: Advanced Features & Optimization ✅ COMPLETED
- [x] Volatility-based position sizing (TDD)
- [x] Backtesting framework (TDD)
- [x] Switch to `shopspring/decimal` for precision-critical calculations
- [x] Audit/Review orchestrator/workflow engine for logging/tracing consistency

### Phase 8: Architectural Refinement & Scalability ✅ COMPLETED
- [x] Evaluate `dbos-transact-golang` for complex durable workflows (See [ADR-001](../adr/001-dbos-workflow-engine.md))
- [x] Draft ADR for decoupled gRPC-based exchange connectors (See [ADR-002](../adr/002-decoupled-exchange-connectors.md))
- [x] Implement gRPC service definitions for exchange adapters (`proto/exchange.proto`)
- [x] Manage protobufs with `buf` CLI
- [x] Implement internal `RemoteExchange` proxy in Go
- [x] Build unified exchange gRPC Connector service (`cmd/exchange_connector`)

### Phase 9: Durable Workflow Migration (DBOS) (Current)
- [x] Install `dbos-transact-golang` SDK
- [x] Refactor `PositionManager` to be deterministic and action-based
- [x] Implement initial DBOS-powered workflows in `internal/workflow/dbos`
- [x] Implement DBOS workflow unit and integration tests (TDD)
- [ ] Migrate SQLite state storage to DBOS-managed structured schema
- [ ] Verify durable execution with chaos testing

### Phase 10: Multi-Exchange Expansion ✅ COMPLETED
- [x] Implement Binance Spot/Futures, OKX (V5), Bybit (V5) Adapters
- [x] Unified CLI/Config/Env interface for all connectors
- [x] Single binary entry for all exchange connectors
- [x] Implement gRPC integration tests for unified connector (TDD)

### Phase 11: Polyglot Connector Validation ✅ COMPLETED
- [x] Implement live market data test for Python connector
- [x] Add comprehensive TDD suite for `ccxt` mapping in Python
- [x] Verify gRPC streaming and unary calls between Go engine and Python connector

### Phase 12: Data Model Normalization & Strict Typing (Current)
- [ ] Refactor `proto/exchange.proto` to use strict Enums for Side, Type, Status, and TimeInForce
- [ ] Update Python connector to strictly map CCXT values to Proto Enums
- [ ] Update Go adapters (Server/Remote) to map Core Enums to Proto Enums
- [ ] Add normalization unit tests in both Go and Python

---

*Last updated: 2026-01-17*
