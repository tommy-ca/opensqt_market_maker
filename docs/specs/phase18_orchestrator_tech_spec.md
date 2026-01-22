# Phase 18.2: Multi-Symbol Orchestrator - Technical Specification

**Project**: OpenSQT Market Maker  
**Status**: Active  
**Pattern**: Sharded Actor Model  
**Technology**: Go + DBOS

---

## 1. Architectural Design

The goal is to transition from a single-symbol bot to a multi-symbol orchestrator that manages independent trading "Actors" with **Stateful Persistence** via DBOS.

### 1.1 Components

#### 1.1.1 `Orchestrator`
The root component responsible for:
- **Registry Management**: Managing the list of active symbols via durable workflows.
- **Recovery**: Recovering the active symbol list from DBOS storage on startup.
- **Multiplexing**: Owning the gRPC streams and routing to symbol actors.

#### 1.1.2 `SymbolManager`
A vertical slice of the trading engine for one symbol.
- **State**: Owns a `PositionManager`.
- **Logic**: Owns a `Strategy`.
- **Execution**: Owns an `Engine` (Simple or Durable).
- **Communication**: Receives events via a buffered channel (`chan *pb.PriceChange`, `chan *pb.OrderUpdate`).

### 1.2 Stateful Orchestrator Design

The Orchestrator itself becomes a DBOS-aware component.

#### 1.2.1 Orchestrator Workflows

1.  **`StartOrchestrator` (Workflow)**:
    - Fetches `ActiveSymbols` from the `symbol_registry` database table.
    - For each symbol, initializes and starts a `SymbolManager` (Actor).
2.  **`AddTradingPair` (Workflow)**:
    - Parameters: Symbol, Config.
    - Transactionally inserts the pair into the `symbol_registry` table.
    - Triggers the creation and startup of the in-memory `SymbolManager`.
3.  **`RemoveTradingPair` (Workflow)**:
    - Transactionally removes the entry from `symbol_registry`.
    - Stops and removes the in-memory `SymbolManager`.

#### 1.2.2 Database Schema (`symbol_registry`)
We will define a new table to store the persistent state of the orchestrator:
| Column | Type | Description |
|--------|------|-------------|
| symbol | VARCHAR(50) | Primary Key (e.g., BTCUSDT) |
| exchange | VARCHAR(50) | Exchange name |
| strategy_config | JSONB | Serialized strategy parameters |
| risk_config | JSONB | Serialized risk parameters |
| status | VARCHAR(20) | ACTIVE, PAUSED, etc. |

#### 1.2.3 Registry Rehydration
On process startup, the `main.go` entry point will call `Orchestrator.Recover()`, which executes a DBOS transaction to read the registry and spawn the necessary actors.

#### 1.2.2 Shared Resource: `IExchange`
The Orchestrator maintains a single `RemoteExchange` instance. This instance is passed to all `SymbolManager`s for execution of REST calls (PlaceOrder, etc.), while the Orchestrator handles the inbound WebSocket/gRPC streams.

### 1.3 Concurrency Model: Sharded Actor

To minimize mutex contention, we avoid global locks.

1. **Routing**: The Orchestrator uses a `map[string]*SymbolManager`. Since this map is built at startup and becomes read-only during operation, it can be accessed without locks in the event loops.
2. **Dispatch**: Each `SymbolManager` runs its own internal `run()` loop in a goroutine. The Orchestrator sends events into that actor's channel.
3. **Isolation**: If a DBOS workflow for BTC hangs or fails, the channel for ETH remains open and processing.

---

## 2. DBOS Workflow Composition

Each `SymbolManager` will initialize its own `DBOSEngine`. This ensures that:
- Workflow IDs are naturally sharded (e.g., `btc-price-update-123`, `eth-price-update-456`).
- Side effects (orders) are tracked per symbol actor.
- State snapshots are saved per symbol.

### 2.1 Workflow Building Blocks
We will reuse the existing `TradingWorkflows` as the building blocks for each Actor.

```go
// internal/trading/orchestrator/manager.go
type SymbolManager struct {
    symbol string
    engine engine.Engine // DBOSEngine or SimpleEngine
    // ...
}
```

---

## 3. Concurrency Optimization (Mutex Minimization)

### 3.1 Current Contention Points
- `PositionManager.mu` is held during entire decision making.
- `LiveServer` reads `PositionManager` state frequently.

### 3.2 Proposed Optimization: Snapshots
We will add `GetSnapshot()` to the `IPositionManager` interface.

```go
// internal/core/interfaces.go
type IPositionManager interface {
    // ...
    GetSnapshot() *pb.PositionManagerSnapshot
}
```

**Implementation Logic**:
1. `GetSnapshot()` acquires `mu.RLock()`.
2. Value-copies all slots and critical counters into a `pb.PositionManagerSnapshot` struct.
3. Releases `mu.RUnlock()`.
4. The caller (Monitoring/Logging) uses the snapshot safely without holding any locks.

---

## 4. Implementation Plan (TDD)

### 4.1 Phase 1: Orchestrator Skeleton
- Define `Orchestrator` and `SymbolManager` structs in `internal/trading/orchestrator`.
- Implement event routing logic.

### 4.2 Phase 2: Refactoring PositionManager
- Implement `GetSnapshot()`.
- Update `SimpleEngine` and `DBOSEngine` to use the new pattern.

### 4.3 Phase 3: Integration
- Update `main.go` to use `orchestrator.AddSymbol` for each entry in the config.
- Verify multi-symbol event flow.

---

## 5. Acceptance Criteria
- [ ] Multiple symbols trading concurrently in one process.
- [ ] Zero impact of one symbol's failure on others.
- [ ] Clean `go test -race` report.
- [ ] Validated gRPC stream multiplexing.
