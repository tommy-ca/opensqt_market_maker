# OpenSQT Core Architecture - Shared Building Blocks

## 1. Overview
OpenSQT is a modular quantitative trading framework supporting multiple specialized agents (`market_maker`, `arbitrage_bot`). This document details the **Shared Core Architecture** used by all agents.

## 2. Shared Data Models (Protobuf)
Defined in `api/proto/v1/`:
- **Resources**: `Order`, `Position`, `Trade`, `FundingRate`.
- **Events**: `PriceChange`, `OrderUpdate`, `FundingUpdate`.
- **State**: `InventorySlot` (Grid), `Leg` (Arbitrage).

## 3. Core Building Blocks

### 3.1 Strategy Logic (Pure)
- **Role**: Stateless, deterministic logic.
- **Implementations**:
  - `grid.Strategy`: Trailing grid calculation (used by `market_maker`).
  - `arbitrage.Strategy`: Spread analysis (used by `arbitrage_bot`).

### 3.2 State Managers
- **Role**: Thread-safe tracking of the agent's footprint.
- **Implementations**:
  - `grid.SlotManager`: Tracks grid levels.
  - `arbitrage.LegManager`: Tracks multi-leg positions.
- **Interface**: `IPositionManager` (for generic observability).

### 3.3 Execution Orchestration
- **Role**: Handles API interaction reliability.
- **Components**:
  - `OrderExecutor`: Rate-limited single order placement.
  - `SequenceExecutor`: Multi-step execution with compensating transactions.
  - `DBOS Workflows`: Durable, resumable execution for production.

### 3.4 Monitors
- **Role**: Real-time data ingestion.
- **Components**:
  - `PriceMonitor`: Websocket price feed.
  - `FundingMonitor`: Funding rate stream.
  - `RiskMonitor`: Global market crash detection.

## 4. Engine Architecture
All agents use an implementation of the `engine.Engine` interface to glue these blocks together:

```go
type Engine interface {
    Start(ctx) error
    Stop() error
    OnPriceUpdate(ctx, price) error
    OnOrderUpdate(ctx, update) error
    OnFundingUpdate(ctx, update) error
    OnPositionUpdate(ctx, position) error
}
```

### 4.1 Simple Engine (Backtest/Dev)
- **Behavior**: Procedural, in-memory state.
- **Persistence**: Periodic checkpoints to SQLite.

### 4.2 Durable Engine (Production)
- **Behavior**: Workflow-based execution using **DBOS**.
- **Persistence**: Transactional state updates, guaranteeing resumption after crash.

## 5. Security & Risk
- **RiskMonitor**: Standardized crash detection circuit breaker.
- **Liquidation Guard**: Automated emergency exit if position nears liquidation price.
- **TLS/Auth**: All remote communication (gRPC) is encrypted and authenticated.
