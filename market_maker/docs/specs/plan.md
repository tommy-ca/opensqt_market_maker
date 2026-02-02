# OpenSQT Market Maker - Roadmap & Implementation Plan

## Phase 1: Core Refactor ✅ COMPLETED
- [x] Consolidate project into a single Go module.
- [x] Implement deterministic Trading and Position Management logic.
- [x] Establish unified interfaces for Exchange, Risk, and Order Execution.
- [x] Implement DBOS-backed durable workflows for production resilience.

## Phase 2: Protobuf-First Unification ✅ COMPLETED
- [x] Define a single source of truth for all data models in `api/proto/v1/models.proto`.
- [x] Establish a strictly generated Go package in `internal/pb`.
- [x] Implement manual conversion helpers in `pkg/pbu` to decouple generated code.
- [x] Refactor core trading logic to speak exclusively in Protobuf models.
- [x] Standardize on unified `engine.Engine` interface.

## Phase 3: Legacy Feature Parity & Verification ✅ COMPLETED
- [x] Documentation & Design Updates.
- [x] Gap Analysis (Order Stream, Risk Monitor, Reconciler).
- [x] Feature Parity Implementation (Cancel All Buys, Risk Checks).
- [x] TDD & Verification (Compilation fixes, Backfill tests).

## Phase 4: High-Performance Adapter Completion ✅ COMPLETED
- [x] **Specifications**: Binance, Bitget, Gate, OKX, Bybit.
- [x] **Implementations**:
    - [x] Binance (REST + WS + Security)
    - [x] Bitget (REST + WS + Security)
    - [x] Gate (REST + WS + Security)
    - [x] OKX (REST + WS + Security)
    - [x] Bybit (REST + WS + Security)
- [x] **Integration**: ExchangeFactory, Config updates.

## Phase 5: Critical Code Review & Production Readiness (Current)
*Addressing critical findings from Multi-Agent Code Review*

### Security & Compliance
- [x] **TLS Encryption**: Implement TLS for gRPC (Server/Client) #001
- [x] **gRPC Authentication**: API Key Interceptor + Rate Limiting #002
- [x] **Credential Management**: Environment variables (12-factor) #003
- [x] **WebSocket CSRF**: Validate Origin header #004 (Resolved)
- [x] **Sensitive Logs**: Redact API keys from logs #018

### Stability & Performance
- [x] **HTTP Client Performance**: Connection pooling #005
- [x] **Unbounded Goroutines**: Implement worker pools/lifecycle management #006 (Resolved)
- [x] **SQLite Transactions**: Atomic State Persistence (Verified) #007
- [x] **Lock Ordering**: Documentation & Prevention #008
- [x] **Remote Reconnect**: Automatic gRPC/Stream reconnection #010 (Resolved)
- [x] **WebSocket Rate Limiting**: Token bucket for WS frames #017
- [x] **WebSocket Goroutine Leak**: Cleanup heartbeat routines (WaitGroup) #019

### Data Integrity
- [x] **Idempotency**: Fix OnOrderUpdate duplicates #009
- [x] **Reconciliation Correction**: Hybrid correction implemented (Auto <5%, Halt >=5%) #021
- [x] **State Ordering**: Ensure DB commit before Memory update (Rollback pattern) #022
- [x] **Reconciliation Race**: Fix read/write races (Snapshot pattern) #023

### Code Quality & Observability
- [x] **Base Adapter**: Reduce duplication #011
- [x] **Metrics**: Prometheus/OpenTelemetry export #013
- [x] **Risk Control API**: gRPC service for risk limits #012, #024
- [x] **Position API**: gRPC service for introspection #025
- [x] **Error Tracking**: Bounded error history (Ring Buffer) #020
- [x] **Unused Base Adapter**: Removed unused code #015
- [x] **Missing Gitignore**: Added .gitignore #016
- [x] **Base Adapter Type Bug**: Fixed receiver type #014

## Phase 6: Strategy Optimization
- [x] **Dynamic Grid Logic**: Adaptive interval based on ATR #strat-1, #strat-2
- [x] **Trend Following**: Inventory skew logic #strat-3
- [x] **Backtesting**: Simulation for dynamic strategy #strat-4

## Phase 7: Hardening & Optimization
- [x] **Concurrency Analysis**: Audit goroutine usage #optim-1
- [x] **Worker Pool Integration**: Integrate `alitto/pond` for bounded concurrency #optim-2
- [x] **Component Migration**:
  - [x] RiskMonitor kline processing #optim-3
  - [x] PositionManager broadcasting #optim-4
  - [x] Exchange stream handling #optim-5
- [x] **End-to-End Testing**: Verify stability under load and fix flaky tests #test-e2e-1

## Phase 8: Final Polish & Release
- [ ] Final Security Audit
- [ ] Performance Benchmarking (Latency/Throughput)
- [ ] User Documentation (Deployment, Configuration)
- [x] **Code Quality**: Fix `go vet` issues (constructor arguments in tests, lock copying)

## Phase 9: Database Schema Management (Atlas) ✅ COMPLETED
- [x] **Research & Setup**:
  - [x] Evaluated `atlas` capabilities for SQLite.
  - [x] Extracted SQLite schema to `schema.hcl`.
- [x] **Migration Workflow**:
  - [x] Generated initial migration for existing schema using `atlas migrate diff`.
  - [x] Established CLI-based migration workflow (removed application dependency on Atlas SDK).
- [x] **Integration & Validation**:
  - [x] Updated `NewSQLiteStore` to assume schema is pre-migrated.
  - [x] Validated automatic migration application via CLI in test environments.
  - [x] Verified schema integrity (check constraints) via Atlas inspection.
  - [x] Verified all existing tests pass with CLI-based migrations.
  - [x] **Validated**: Confirmed zero application dependencies on Atlas SDK.
  - [x] **Validated**: Updated deployment guide with standalone migration instructions.
  - [x] **Validated**: Removed `ariga.io/atlas-go-sdk` from project dependencies.

## Phase 10: Unified Observability & E2E Audit ✅ COMPLETED
- [x] **gRPC Client Integration**: Implemented `market_maker` gRPC client in `live_server` #obs-1
- [x] **Stream Refactoring**: Pivoted `live_server` to consume trading data via gRPC #obs-2
- [x] **Workflow Audit**: Reviewed `live_server` reconnection and startup sequences #obs-4
- [x] **UI Enrichment**: Mapped internal engine slots to WebSocket messages for UI #obs-3
- [x] **E2E Stability**: Created comprehensive observability E2E test suite #test-e2e-2

## Phase 12: Architectural Refinement & Strategy Generalization ✅ COMPLETED
- [x] **Building Blocks**:
  - [x] Extracted pure logic to `arbitrage.Strategy`.
  - [x] Extracted state management to `arbitrage.LegManager`.
  - [x] Extracted execution orchestration to `arbitrage.SimpleExecutor`.
- [x] **Modular Engines**:
  - [x] Refactored `ArbitrageEngine` (Simple) to use building blocks.
  - [x] Refactored `DBOSArbitrageEngine` (Durable) to use building blocks.
- [x] **Protobuf Enhancements**:
  - [x] Added `Exchange` field to `OrderUpdate` for multi-exchange routing.
  - [x] Added `LiquidationPrice` to `Position` for risk monitoring.

## Phase 13: Final Audit & Production Hardening ✅ COMPLETED
... (existing content) ...

## Phase 14: Grid Strategy Refactor (Building Blocks) ✅ COMPLETED
- [x] **Grid Logic Extraction**:
  - [x] Created `grid.Strategy` (pure logic) for active window and action calculation.
  - [x] Implemented TDD for trailing grid logic and ATR-based intervals.
- [x] **State Manager Refactor**:
  - [x] Created `grid.SlotManager` to handle slot state transitions and order mapping.
  - [x] Implemented robust state recovery from snapshots.
- [x] **Modular Orchestration**:
  - [x] Created `gridengine.GridEngine` as a lean orchestrator.
  - [x] Integrated `gridengine` into `main.go` (unifying Grid and Arb patterns).
- [x] **Execution Standardization**:
  - [x] Reused `OrderExecutor` for physical order placement.
  - [x] Implemented compensating transactions in `SimpleExecutor` for multi-leg safety.

## Phase 15: Production Hardening & E2E Validation ✅ COMPLETED
- [x] **E2E Stability**: Verified system stability with existing integration tests.
- [x] **Durable Grid Engine**: Initial implementation of `DBOSGridEngine` using building blocks.
- [x] **Monitoring**: Standardized gRPC interfaces for grid and arb state.

## Phase 16: Arbitrage Bot Extraction & Multi-Binary Architecture ✅ COMPLETED
- [x] **Project Restructuring**:
  - [x] Defined `cmd/arbitrage_bot` entry point.
  - [x] Modularized shared building blocks (Logic, State, Execution).
- [x] **Arbitrage Bot Implementation**:
  - [x] Standalone binary implemented and verified.
  - [x] Decoupled config usage in engines.
- [x] **Modular Verification**:
  - [x] Generalized `SequenceExecutor` for multi-leg atomicity.

## Phase 17: Production Hardening & Global Risk Unification ✅ COMPLETED
- [x] **E2E Stability**: Fix `no such table: state` in `TestE2E_CrashRecovery` by ensuring schema migration in test helpers.
- [x] **Unified Risk Interface**:
  - [x] Integrate `RiskMonitor` into `ArbEngine` to halt *new entries* during global market anomalies.
  - [x] Standardize `CircuitBreaker` usage across both engines.
- [ ] **Observability**: Expose arbitrage spread and leg delta metrics via Prometheus/OTel.

