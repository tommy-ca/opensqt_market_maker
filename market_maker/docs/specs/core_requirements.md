# OpenSQT Market Maker - Functional Requirements & Workflows

> **Source**: Extracted from Legacy v3.3.1 Architecture and implementation.
> **Status**: Living document for Market Maker refactor.

## 1. Core Principles

### 1.1 Single Price Source
... (existing content) ...

## 16. Uniform Risk Management
- **Requirement**: All strategy engines (Grid, Arbitrage, etc.) MUST implement a consistent interface for Risk Monitor integration.
- **Workflow**:
  1. Engine receives market data update.
  2. Engine queries `RiskMonitor.IsTriggered()`.
  3. If triggered, Engine MUST halt new order placement and execute defensive actions (e.g., Cancel All Buy Orders).
  4. Engine receives periodic position updates and queries `Strategy.ShouldEmergencyExit()`.
  5. If emergency signaled, Engine MUST execute immediate exit workflow.

## 17. Multi-Binary Architecture
...
- **Requirement**: The system MUST support multiple specialized binary entry points (e.g., `market_maker`, `arbitrage_bot`) while sharing a common core library.
- **Goal**: Isolation of concerns, independent deployment, and reduced attack surface for each agent.
- **Constraints**: 
  - Shared code MUST reside in `internal/trading/` or `pkg/`.
  - Binary-specific glue code MUST reside in `cmd/`.

## 18. Formal Strategy Decomposition
- **Requirement**: Every trading strategy MUST be decomposed into three distinct layers:
  1. **Logic**: Stateless, deterministic, I/O-free math that decides *what* to do.
  2. **State**: Thread-safe manager that tracks *current footprint* (orders/positions).
  3. **Execution**: Sequential or parallel orchestrator that handles *API interactions*.
- **Benefit**: Simplifies testing (100% logic coverage) and reuse (share LegManager between basis and arb bots).
- **Requirement**: The system MUST have exactly one source of truth for market price (`PriceMonitor`).
- **Implementation**:
  - WebSocket is the ONLY allowed source for real-time price updates.
  - REST API polling for price is strictly FORBIDDEN during normal operation (latency unacceptable).
  - All components must query `PriceMonitor` or subscribe to its broadcast channel.
  - **Constraint**: System must not start trading until the first WebSocket price is received.

### 1.2 Order Stream Priority
- **Requirement**: Order update stream MUST be active and verified BEFORE any orders are placed.
- **Workflow**:
  1. Start Exchange Order Stream (WebSocket).
  2. Verify Stream Connection.
  3. Start Position Manager / Trading Logic.
  4. Place Orders.
- **Rationale**: Prevents "phantom orders" where the system places an order but misses the "Filled" execution report because the listener wasn't ready, leading to state desynchronization.

### 1.3 Slot-Based Inventory Management
- **Requirement**: Trading MUST be organized into "Slots" (Grid Levels) with strict state locking.
- **Dynamic Grid (Trailing)**: The grid MUST track the market price.
    - **Grid Center**: Calculated as `Round(CurrentPrice / Interval) * Interval`.
    - **Active Window**: Only slots within `[GridCenter - BuyWindow*Interval, GridCenter - Interval]` (for Buys) and `[GridCenter + Interval, GridCenter + SellWindow*Interval]` (for Sells) are active for *new* order placement.
    - **Persistence**: Slots outside the window with existing positions MUST be retained to facilitate position closing.
- **States**:
  - `FREE`: Empty, available for new orders.
  - `PENDING`: Order request sent, awaiting `New` status from exchange.
  - `LOCKED`: Active order exists on exchange (Open Order).
- **Constraint**: No operation can be performed on a slot unless it is `FREE` (for new orders) or `LOCKED` (for cancels/amends).
- **Locking**: Each slot must have its own mutex to allow concurrent processing of different price levels while protecting individual state.

### 1.4 Fixed Amount Trading
- **Requirement**: The system operates on a "Fixed Amount" basis (e.g., 30 USDT per grid level), NOT "Fixed Quantity".
- **Calculation**: `Quantity = OrderAmount / Price`.
- **Rationale**: Ensures consistent capital deployment across different price ranges.

### 1.5 Multi-Exchange Architecture
- **Requirement**: The system MUST be capable of instantiating and managing connections to multiple exchanges simultaneously within a single runtime.
- **Market Discovery**: The system MUST support bulk fetching of market statistics (24h Volume, Price Change) to enable automated universe selection and liquidity filtering.
- **Context**: Required for Arbitrage strategies (see Section 13).
- **Constraint**: Each exchange instance must maintain isolated rate limiters and authentication contexts.
- **Support**: Must support both **Cross-Exchange** (e.g., Binance + Bybit) and **Same-Exchange Multi-Product** (e.g., Binance Spot + Binance Futures) configurations.

---

## 2. Safety & Risk Control

### 2.1 Pre-Flight Safety Checks (Startup)
Before entering the trading loop, the system MUST verify:
1.  **Account Balance**: Sufficient to cover `RequiredPositions * OrderAmount` on **ALL** active exchanges (for Arbitrage).
2.  **Leverage**: Current leverage does not exceed max allowed (e.g., 10x) on all exchanges.
3.  **Fee Rate**: Fee rate is fetched and acceptable (supports 0 fee).
4.  **Connectivity**: WebSocket streams (Price, Order, Funding) are healthy for all connections.

### 2.2 Active Risk Monitor (Runtime)
- **Requirement**: Real-time monitoring of market conditions.
- **Dual Trigger Condition**: Risk Control is triggered only when **BOTH** conditions are met:
  1.  **Price Drop**: `CurrentPrice < AveragePrice` (indicating downtrend/crash).
  2.  **Volume Spike**: `CurrentVolume > AverageVolume * Multiplier` (indicating panic selling).
- **Action**:
  - If Triggered: **IMMEDIATELY CANCEL ALL BUY ORDERS**.
  - Pause new order placement.
  - Resume only when conditions normalize (e.g., majority of monitored assets recover).

### 2.3 Reconciliation (Self-Healing)
- **Requirement**: Periodic sync between Local State (Slots) and Exchange State.
- **Interval**: Configurable (e.g., every 60s).
- **Checks**:
  - Orphaned Orders: Orders on exchange but not in a Slot -> Cancel.
  - Missing Orders: Slot thinks order is open, but exchange has no order -> Reset Slot to FREE.
  - Balance Sync: Update local balance view.
  - **Correction Strategy**:
    - **Small Divergence (< 5%)**: Automatically correct local state via `ForceSync`.
    - **Large Divergence (>= 5%)**: Trigger Circuit Breaker to HALT trading and alert operations.

### 2.4 Order Cleanup
- **Requirement**: Prevent accumulation of stale orders.
- **Logic**: If `OpenOrders > Threshold`, cancel the oldest/furthest orders to free up slots and API limits.

---

## 3. Order Execution

### 3.1 Robust Execution
- **Rate Limiting**: Must respect exchange limits (e.g., 10 orders/sec).
- **Retry Mechanism**:
  - Network errors: Retry with backoff.
  - Logic errors (insufficient funds): Do not retry immediately; trigger safety lock.
- **Post-Only Downgrade**:
  - Try `PostOnly` first.
  - If rejected (would cross book), retry `n` times.
  - If still failing, optionally downgrade to `Taker` (if config allows) or pause slot.

### 3.2 Batch Operations
- **Requirement**: Support batch order placement/cancellation where exchange API permits (e.g., Bitget).
- **Fallback**: Sequential async execution for exchanges without native batch support.

### 3.3 Security & Integrity
- **TLS Encryption**: All gRPC communications must use TLS 1.3+.
- **Authentication**: gRPC endpoints must be protected by API Key authentication with rate limiting.
- **Data Integrity**: State persistence must use atomic transactions (SQLite WAL mode).
- **Idempotency**: All order updates must be idempotent to handle WebSocket replays.

---

## 4. System Architecture

### 4.1 Concurrency Model
- **Main Loop**: Non-blocking, event-driven (Price Updates).
- **Order Updates**: Callback-based (WebSocket -> Channel -> State Update).
- **State Safety**: 
  - `sync.RWMutex` for global map access.
  - Granular locks for individual Slot modification.
  - **Lock Ordering**: Strict hierarchy (Global -> Slot) to prevent deadlocks.
  - Atomic values for high-frequency reads (LastPrice).
  - **Goroutine Management**: All background goroutines must be bounded and strictly managed (no leaks).

### 4.2 Configuration
- **Dynamic**: Support reloading crucial settings without restart (nice-to-have).
- **Secrets**: API Keys/Secrets must be handled securely (Env vars or secure config, never logged).

## 5. Exchange Support
The system must support the following primary exchanges via a unified `IExchange` interface:
1.  **Binance Futures** (USDT-M)
2.  **Bitget Futures** (USDT-M)
3.  **Gate.io Futures** (USDT-M)
4.  **OKX Futures** (Swap)
5.  **Bybit Futures** (Linear)

## 6. Security & Safety Requirements (OpenCode Integration)
- **No Malicious Code**: The system MUST NOT contain or facilitate any code that could be used maliciously, including but not limited to malware, hacking tools, or unauthorized access mechanisms.
- **Secret Management**: API keys and sensitive credentials MUST be handled securely using environment variables or secure configuration, never logged or committed to version control.
- **Input Validation**: All user inputs, configuration parameters, and external data MUST be validated to prevent injection attacks or unexpected behavior.
- **Error Handling**: Comprehensive error handling without exposing sensitive information in logs or responses.
- **Access Control**: Implement proper authentication and authorization for any administrative or configuration interfaces.
- **Audit Trail**: Maintain secure logs for all trading operations and system changes, ensuring compliance with regulatory requirements.

## 7. Code Quality & Best Practices
- **Go Vet Compliance**: All code MUST pass `go vet` without warnings. Protobuf messages MUST be passed by pointer to avoid mutex copying issues.
  - **Implementation Status**: Phase 9 Complete - 98% compliance achieved (100+ warnings → 2 acceptable)
  - **Remaining**: 2 warnings in main.go (acceptable - dereferencing pointers for engine interface calls)
- **Test Coverage**: Minimum 80% code coverage for core trading logic.
  - **Current**: Exchange adapters, trading components, risk management all tested
- **Linting**: Code MUST pass `golint` and `staticcheck` with no critical issues.
- **Documentation**: All public interfaces and complex logic MUST be documented with clear comments.
- **Thread Safety**: All concurrent operations must use proper synchronization primitives.
  - **Implementation**: Pointer-based protobuf messages prevent mutex copying
  - **Validation**: Go vet ensures lock safety across 40+ files

## 8. Agent Integration & Observability API
- **Requirement**: The system MUST expose its internal state via gRPC services to enable external agents (human or AI) to monitor and control the system safely.
- **Risk Service**:
  - **Status Query**: Ability to check if Risk Monitor is triggered and why.
  - **Circuit Breaker**: Ability to query state (Open/Closed) and manually trip/reset.
  - **Metrics**: Expose risk scores, exposure levels, and position limits.
- **Position Service**:
  - **Introspection**: Ability to query all open positions, active orders, and recent fills.
  - **History**: Access to PnL history and trade records.
  - **Streaming**: Real-time subscription to position changes.
- **Security**: All agent-facing APIs must be protected by the same authentication mechanisms as the trading interfaces.

## 9. Dynamic Strategy Optimization
- **Dynamic Grid Intervals**:
  - The system MUST support adaptive grid intervals based on market volatility (ATR).
  - The interval should expand in high volatility to reduce risk and transaction costs.
  - Configuration should allow enabling/disabling this feature and setting scaling factors.
- **Trend Following (Inventory Skew)**:
  - The system MUST support skewing grid levels based on current inventory.
  - If inventory is high (Long), the grid should shift downwards to facilitate selling and buy at lower prices.
  - If inventory is low/short, the grid should shift upwards.

## 10. Resource Management & Reliability
- **Bounded Concurrency**:
  - The system MUST use bounded worker pools for processing asynchronous tasks (e.g., market data updates, event broadcasting) to prevent goroutine leaks and unbounded memory usage during high load.
  - **Library**: Use a proven library like `alitto/pond` or `goptics/varmq`.
- **Graceful Degradation**:
  - If worker pools are full, the system should either drop non-critical events (with logging) or apply backpressure, rather than crashing.

## 11. Schema Management (Atlas)
- **Requirement**: The system MUST use `atlas` (ariga/atlas) to manage database schemas and migrations for any persistent storage that is **NOT** managed by the DBOS engine.
- **Scope**:
  - Initial focus on the SQLite schema used in `internal/engine/simple/store_sqlite.go`.
- **Workflow**:
  - Schema is defined in `schema.hcl`.
  - Migrations are versioned in `migrations/` directory.
  - Deployment MUST apply migrations using the `atlas` CLI before system startup.
  - The application itself MUST NOT depend on Atlas libraries or SDKs to maintain a minimal dependency footprint.

## 12. Unified Observability & UI Consistency
- **Requirement**: The `live_server` (UI) MUST reflect the internal state of the `market_maker` engine as the single source of truth for trading activity.
- **Data Flow**:
  - `live_server` MUST consume Position and Risk data via the `market_maker` gRPC API.
  - Direct exchange WebSocket connections in `live_server` should be limited to high-bandwidth market data (K-lines) to reduce API load and ensure consistency.
- **UI Enrichment**:
  - The UI MUST display internal slot statuses (e.g., `PENDING`, `LOCKED`, `FREE`).
  - The UI MUST display real-time Risk Monitor status and volatility scores.
- **Resilience**:
  - The `live_server` MUST implement robust reconnection logic for gRPC streams to handle `market_maker` restarts.

## 14. Modular Strategy Building Blocks
- **Requirement**: Trading strategies MUST be built using shared, modular components to ensure code reuse and consistent behavior across different strategy types.
- **Components**:
  - **Strategy Logic**: Pure function/method that maps market data to abstract actions (Entry, Exit, Skew).
  - **State Manager**: Component that tracks the specific state required for the strategy (e.g., Inventory Slots for Grid, Legs for Arbitrage).
  - **Executor**: Orchestrator that handles the physical execution of multiple orders, including compensating transactions for partial failures.
- **Principle**: Strategies should be "Lean Orchestrators" that glue these building blocks together.

## 15. Fault-Tolerant Multi-Leg Execution
- **Requirement**: Any multi-leg operation (e.g., Arbitrage Entry) MUST implement a compensation pattern to ensure the system returns to a safe state if any leg fails.
- **Workflow**:
  1. Attempt Leg A.
  2. If Leg A fails, abort.
  3. Attempt Leg B.
  4. If Leg B fails, trigger compensation for Leg A (e.g., Unwind).
- **Durability**: For production use, these sequences MUST be implemented as durable workflows (e.g., DBOS) to handle process crashes between steps.
