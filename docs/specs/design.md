# System Design & Architecture

## 1. Architectural Overview

The OpenSQT Market Maker adopts a modular, microservices-like architecture where the **Trading Engine** and **Exchange Connectors** are decoupled via **gRPC**.

```mermaid
graph TD
    A[Trading Engine] -->|Internal Call / gRPC| B[Exchange Connector]
    B -->|REST/WS| C[Crypto Exchange]
    A -->|State Persistence| D[SQLite Database]
    B -->|Market Data| A
```

### 1.1 Native vs Remote Connectors
The system is designed for a **Native-First** approach:
- **Native Path**: Go-native adapters (Binance, Bitget, Gate, OKX, Bybit) are linked directly into the `market_maker` binary. This path eliminates gRPC overhead and provides the lowest possible latency.
- **Remote Path**: For exchanges without a Go adapter, the engine connects to a remote gRPC sidecar (e.g., the Python Connector). This is enabled by setting `current_exchange: remote` in the config.

## 2. Exchange Connector

### 2.1 Responsibilities
- **Protocol Translation**: Map gRPC/Internal requests to REST/WebSocket API calls.
- **Normalization**: Standardize order status, sides, and error codes.
- **Resilience**: Handle WebSocket reconnections and API rate limiting.

### 2.2 Interface Design
- **Schema**: Defined in `api/proto/opensqt/market_maker/v1/`.
- **Standardization**: Managed via the **Buf CLI**. All field names follow Go/Python idiomatic casing (e.g., `OrderId`, `ClientOrderId`).

## 3. Trading Engine (Core)

### 3.1 Durable Workflows
The core logic relies on a durable execution model to ensure reliability and crash recovery.
- **Simple Implementation**: The `SimpleEngine` persists state transitions to a SQLite database (`Store`) *before* acknowledging completion.
- **DBOS Implementation**: The `DBOSEngine` leverages the DBOS runtime to provide true durable workflows. Decisions and side effects are wrapped in `ctx.RunWorkflow` and `ctx.RunAsStep`, providing exactly-once execution and durable progress.
- **Guarantee**: On crash, the system reloads the grid and reconciles with the exchange via `RestoreFromExchangePosition`.

### 3.2 Modular Strategy Execution
The strategy layer is completely decoupled from the engine:
- **`IStrategy`**: A pure logic interface.
- **`GridStrategy`**: Implementation of a high-frequency grid trading logic with `Long` and `Neutral` (Market Making) modes.

### 3.3 Health Monitoring
The system uses a centralized `HealthManager` to aggregate status from all modules.
- **Components**: Each critical module (PriceMonitor, RiskMonitor, etc.) registers a health check function.
- **Aggregation**: The manager periodically executes checks and determines global health.
- **gRPC Sidecars**: The engine uses the standard gRPC Health Checking Protocol to monitor the availability of remote connectors.
- **Endpoints**:
    - `GET /health`: Liveness probe for orchestrators (e.g., Docker/K8s).
    - `GET /status`: Detailed diagnostic data in JSON format.

## 4. Production Deployment

### 4.1 Containerization
The entire stack is containerized for consistent deployment across environments.
- **Market Maker (Go)**: A high-performance, minimal image based on Distroless.
- **Python sidecar (Optional)**: Only deployed if using Python-specific connectors.

### 4.2 Local Orchestration (Docker Compose)
Standard deployment uses Docker Compose to manage the engine, persistent volumes, and optional sidecars.

```yaml
services:
  market-maker:
    image: opensqt/market-maker:latest
    volumes:
      - ./data:/app/data
      - ./configs:/app/configs
    environment:
      - EXCHANGE=binance
```

### 4.3 Persistence
All critical state (inventory, active orders) is stored in a SQLite database located in a persistent volume. This allows the bot to be upgraded or restarted without losing track of current positions.

## 5. End-to-End Validation Architecture
E2E tests utilize the `SimulatedExchange` and a dedicated test engine instance.
- **Scenario Injection**: Tests inject specific sequences of prices and volumes.
- **State Inspection**: After each event, the test suite verifies both the in-memory `PositionManager` and the SQLite `Store`.
- **Concurrency Test**: Simulate rapid fire updates to ensure mutexes and atomic operations prevent race conditions.

## 6. Live Monitoring & Visualization Architecture (Phase 15 - Standalone Binary)

### 6.1 Overview
A **standalone live_server binary** built on proven market_maker components. This architecture provides complete isolation between trading and monitoring while maximizing code reuse.

### 6.2 Architecture Diagram
```mermaid
graph TD
    subgraph "Shared Libraries (pkg/)"
        A[pkg/exchange/binance]
        B[pkg/exchange/bitget]
        C[pkg/exchange/gate]
        D[pkg/liveserver/hub]
        E[pkg/liveserver/server]
        F[pkg/logging]
    end
    
    subgraph "market_maker Binary"
        G[Trading Engine]
        H[Grid Strategy]
        I[Risk Management]
    end
    
    subgraph "live_server Binary"
        J[WebSocket Hub]
        K[Stream Handlers]
        L[Frontend Server]
    end
    
    M[Crypto Exchange]
    N[Browser Clients]
    
    G --> A
    J --> A
    J --> D
    L --> E
    A --> M
    L --> N
```

### 6.3 Component Design

#### 6.3.1 Shared Library Structure

**pkg/exchange/** (Reusable Exchange Connectors)
```
pkg/exchange/
├── interface.go        # IExchange interface
├── factory.go          # Exchange creation factory
├── binance/
│   ├── binance.go
│   ├── websocket.go
│   └── binance_test.go
├── bitget/
├── gate/
├── okx/
└── bybit/
```

**Key Interface**:
```go
package exchange

type IExchange interface {
    // Identity
    GetName() string
    CheckHealth(ctx context.Context) error
    
    // Streams
    StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(*pb.Candle)) error
    StartOrderStream(ctx context.Context, callback func(*pb.OrderUpdate)) error
    
    // REST
    GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error)
    GetOpenOrders(ctx context.Context, symbol string) ([]*pb.Order, error)
    GetAccount(ctx context.Context) (*pb.Account, error)
    GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error)
}
```

**pkg/liveserver/** (WebSocket Hub Pattern)
```
pkg/liveserver/
├── hub.go              # Broadcast hub
├── hub_test.go
├── messages.go         # Message type definitions
├── messages_test.go
├── server.go           # HTTP + WebSocket server
├── server_test.go
├── client.go           # Client wrapper
└── auth.go             # Optional authentication
```

**Hub Interface**:
```go
package liveserver

type Hub interface {
    Run(ctx context.Context)
    Register(client *Client)
    Unregister(client *Client)
    Broadcast(msg Message)
    ClientCount() int
}

type Message struct {
    Type string      `json:"type"`
    Data interface{} `json:"data"`
}
```

#### 6.3.2 live_server Binary Structure
```
cmd/live_server/
├── main.go             # Entry point
├── streams.go          # Exchange stream handlers
├── config.go           # Configuration loading
└── handlers.go         # HTTP handlers
```

**main.go Structure**:
```go
package main

func main() {
    // 1. Parse flags
    configFile := flag.String("config", "configs/live_server.yaml", "Config file")
    port := flag.String("port", ":8081", "Server port")
    
    // 2. Initialize logger
    logger := logging.NewZapLogger("INFO")
    
    // 3. Load configuration
    cfg := loadConfig(*configFile)
    
    // 4. Create exchange (read-only)
    exch, err := exchange.NewExchange(cfg.Exchange.Name, cfg, logger)
    
    // 5. Create WebSocket hub
    hub := liveserver.NewHub(logger)
    go hub.Run(context.Background())
    
    // 6. Start exchange stream handlers
    go streamKlines(exch, hub, cfg)
    go streamOrders(exch, hub)
    go streamAccount(exch, hub)
    
    // 7. Start HTTP/WebSocket server
    server := liveserver.NewServer(hub, logger)
    server.Start(*port)
}
```

**Stream Handlers**:
```go
// streams.go
func streamKlines(exch exchange.IExchange, hub liveserver.Hub, cfg *Config) {
    ctx := context.Background()
    
    exch.StartKlineStream(ctx, []string{cfg.Symbol}, "1m", func(candle *pb.Candle) {
        hub.Broadcast(liveserver.Message{
            Type: "kline",
            Data: map[string]interface{}{
                "time":   candle.OpenTime / 1000,
                "open":   candle.Open,
                "high":   candle.High,
                "low":    candle.Low,
                "close":  candle.Close,
                "volume": candle.Volume,
            },
        })
    })
}

func streamOrders(exch exchange.IExchange, hub liveserver.Hub) {
    ctx := context.Background()
    
    exch.StartOrderStream(ctx, func(update *pb.OrderUpdate) {
        hub.Broadcast(liveserver.Message{
            Type: "orders",
            Data: map[string]interface{}{
                "id":     update.OrderId,
                "price":  update.Price,
                "side":   update.Side,
                "status": update.Status,
                "symbol": update.Symbol,
            },
        })
        
        // Trade event on fill
        if update.Status == "FILLED" {
            hub.Broadcast(liveserver.Message{
                Type: "trade_event",
                Data: map[string]interface{}{
                    "side":   strings.ToLower(update.Side),
                    "price":  update.Price,
                    "amount": update.Quantity,
                    "symbol": update.Symbol,
                    "time":   time.Now().Unix(),
                },
            })
        }
    })
}
```

### 6.4 Configuration Design

**live_server.yaml**:
```yaml
# Exchange configuration
exchange:
  name: "binance"  # binance, bitget, gate, okx, bybit
  
# Exchange-specific credentials
binance:
  api_key: "${BINANCE_API_KEY}"
  secret_key: "${BINANCE_SECRET_KEY}"
  
bitget:
  api_key: "${BITGET_API_KEY}"
  secret_key: "${BITGET_SECRET_KEY}"
  passphrase: "${BITGET_PASSPHRASE}"

# Trading symbol
trading:
  symbol: "BTCUSDT"
  
# Server configuration
server:
  port: ":8081"
  enable_auth: false
  jwt_secret: "${JWT_SECRET}"
  allowed_origins:
    - "http://localhost:3000"
    - "https://opensqt.com"
    
# Logging
logging:
  level: "INFO"
  format: "json"
```

### 6.5 Message Type Specifications

All messages follow the format: `{"type": string, "data": object}`

**1. kline** - Real-time candlestick updates
```json
{
  "type": "kline",
  "data": {
    "time": 1705881600,
    "open": "42150.50",
    "high": "42200.00",
    "low": "42100.00",
    "close": "42180.25",
    "volume": "125.45"
  }
}
```

**2. account** - Balance updates
```json
{
  "type": "account",
  "data": {
    "asset": "USDT",
    "free": "10000.00",
    "balance": "12000.00",
    "marginBalance": "12000.00",
    "symbol": "BTCUSDT"
  }
}
```

**3. orders** - Order status changes
```json
{
  "type": "orders",
  "data": {
    "id": "12345678",
    "price": "42000.00",
    "side": "BUY",
    "status": "FILLED",
    "type": "LIMIT",
    "symbol": "BTCUSDT"
  }
}
```

**4. trade_event** - Trade execution notification
```json
{
  "type": "trade_event",
  "data": {
    "side": "buy",
    "price": "42000.00",
    "amount": "0.01",
    "symbol": "BTCUSDT",
    "time": 1705881600
  }
}
```

**5. position** - Futures position updates
```json
{
  "type": "position",
  "data": {
    "symbol": "BTCUSDT",
    "amount": "0.5",
    "entryPrice": "41000.00",
    "unrealizedPnL": "590.00"
  }
}
```

**6. history** - Initial historical candles
```json
{
  "type": "history",
  "data": [
    {"time": 1705875600, "open": "41900.00", ...},
    {"time": 1705875660, "open": "41950.00", ...},
    ...
  ]
}
```

### 6.6 Deployment Architecture

#### 6.6.1 Standalone Deployment
```bash
# Build binaries
cd market_maker
go build -o bin/market_maker cmd/market_maker/main.go
go build -o bin/live_server cmd/live_server/main.go

# Run independently
./bin/market_maker --config configs/market_maker.yaml
./bin/live_server --config configs/live_server.yaml --port :8081
```

#### 6.6.2 Docker Deployment
**Dockerfile.live_server**:
```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o live_server cmd/live_server/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /build/live_server .
COPY web/ ./web/
COPY configs/ ./configs/
EXPOSE 8081
CMD ["./live_server", "--config", "configs/live_server.yaml"]
```

**docker-compose.yml**:
```yaml
version: '3.8'

services:
  market-maker:
    build:
      context: ./market_maker
      dockerfile: Dockerfile
    volumes:
      - ./data:/app/data
      - ./configs/market_maker.yaml:/app/config.yaml
    environment:
      - BINANCE_API_KEY=${BINANCE_API_KEY}
      - BINANCE_SECRET_KEY=${BINANCE_SECRET_KEY}
    ports:
      - "8080:8080"
      
  live-server:
    build:
      context: ./market_maker
      dockerfile: Dockerfile.live_server
    volumes:
      - ./configs/live_server.yaml:/app/config.yaml
    environment:
      - BINANCE_API_KEY=${BINANCE_API_KEY}
      - BINANCE_SECRET_KEY=${BINANCE_SECRET_KEY}
    ports:
      - "8081:8081"
```

### 6.7 Benefits of Standalone Architecture

#### 6.7.1 Complete Isolation
- **Failure Isolation**: Frontend crashes don't affect trading
- **Process Isolation**: Separate memory spaces
- **Independent Lifecycles**: Restart monitoring without stopping trading

#### 6.7.2 Flexible Deployment
- **Trading Only**: Run market_maker for headless trading
- **Monitoring Only**: Run live_server to monitor existing positions
- **Full Stack**: Run both together

#### 6.7.3 Independent Scaling
- **Multiple Viewers**: Run N live_servers for different symbols
- **Geographic Distribution**: Deploy live_servers closer to users
- **Resource Allocation**: Scale monitoring independent of trading

#### 6.7.4 Code Reuse
- **DRY Principle**: Single source of truth in pkg/
- **Consistent Behavior**: Both binaries use same exchange logic
- **Easier Maintenance**: Fix once, benefits both binaries

### 6.8 Testing Strategy

#### 6.8.1 Shared Library Tests
```go
// pkg/exchange/binance/binance_test.go
func TestBinanceExchangeInterface(t *testing.T) {
    var _ exchange.IExchange = &BinanceExchange{}
}

// pkg/liveserver/hub_test.go
func TestHubBroadcast(t *testing.T) {
    hub := NewHub(nil)
    client := newMockClient()
    hub.Register(client)
    
    msg := Message{Type: "kline", Data: map[string]interface{}{"price": "42000"}}
    hub.Broadcast(msg)
    
    assert.Equal(t, msg, client.LastMessage())
}
```

#### 6.8.2 Binary Integration Tests
```go
// cmd/live_server/main_test.go
func TestLiveServerE2E(t *testing.T) {
    // Start server in background
    go main()
    time.Sleep(100 * time.Millisecond)
    
    // Connect WebSocket
    ws, _, err := websocket.DefaultDialer.Dial("ws://localhost:8081/ws", nil)
    require.NoError(t, err)
    defer ws.Close()
    
    // Should receive history message
    var msg liveserver.Message
    err = ws.ReadJSON(&msg)
    require.NoError(t, err)
    assert.Equal(t, "history", msg.Type)
}
```

### 6.9 Migration Path

#### 6.9.1 Phase 1: Extract to pkg/
1. Move `market_maker/internal/exchange/` → `pkg/exchange/`
2. Create `pkg/liveserver/` with hub, server, messages
3. Verify market_maker still builds and tests pass

#### 6.9.2 Phase 2: Build live_server Binary
1. Create `cmd/live_server/main.go`
2. Implement stream handlers
3. Add configuration support
4. Test with Binance

#### 6.9.3 Phase 3: Add All Exchanges
1. Test with Bitget, Gate, OKX, Bybit
2. Normalize message formats across exchanges
3. Add exchange-specific features (futures positions, etc.)

#### 6.9.4 Phase 4: Production Deployment
1. Build Docker images
2. Create docker-compose configuration
3. Deploy to staging
4. Monitor and optimize
5. Deploy to production

### 6.10 Observability

#### 6.10.1 Metrics
```go
// Prometheus metrics
var (
    connectedClients = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "live_server_connected_clients",
        Help: "Number of connected WebSocket clients",
    })
    
    messagesSent = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "live_server_messages_sent_total",
        Help: "Total number of messages sent",
    }, []string{"type"})
    
    broadcastLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
        Name: "live_server_broadcast_latency_ms",
        Help: "Broadcast latency in milliseconds",
    })
)
```

#### 6.10.2 Health Endpoint
```go
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    status := map[string]interface{}{
        "status": "healthy",
        "clients": s.hub.ClientCount(),
        "uptime": time.Since(s.startTime).Seconds(),
    }
    
    json.NewEncoder(w).Encode(status)
}
```

### 6.12 gRPC-First Architecture (Production)

Starting from Phase 16, the system enforces a **gRPC-First** architecture for all production deployments.

- **Centralized Connector**: `exchange_connector` handles all WebSocket and REST connections to the exchange.
- **Client Binaries**: `market_maker` and `live_server` act as gRPC clients to the `exchange_connector`.
- **Benefits**:
  - Reduced connection overhead on exchanges.
  - Better fault isolation (connector crash doesn't stop strategy engine immediately).
  - Support for multi-language adapters (Go/Python).
  - Easier monitoring of connection health via standard gRPC health probes.

### 6.14 Multi-Symbol Orchestration (Phase 18)

Starting from Phase 18, the `market_maker` binary evolves into a multi-symbol orchestrator adopting a **Sharded Actor Model** with **Recursive Durability**.

- **Stateful Orchestrator**: A top-level component that manages the lifecycle of multiple `SymbolManager` instances. It uses DBOS to persist the registry of active symbols, ensuring that the system recovers its full trading surface after a restart.
- **SymbolManager (Actor)**: A self-contained vertical trading slice for a specific symbol. It owns its own instance of:
    - `IPositionManager`: Tracks symbol-specific grid state.
    - `IStrategy`: Executes symbol-specific grid logic.
    - `Engine`: Executes durable DBOS workflows for trading logic.
- **Event Routing**: The Orchestrator multiplexes a single gRPC `SubscribePrice` and `SubscribeOrders` stream. Incoming events are routed to the corresponding `SymbolManager` via non-blocking channels.
- **Shared Connection**: All managers communicate with the exchange via a single `RemoteExchange` instance.

### 6.15 Recursive Durability Pattern

The system implements durability at two levels:

1.  **Orchestrator Level (System State)**: Durable workflows for `AddSymbol` and `RemoveSymbol` ensure the symbol registry is always consistent and persistent in the database.
2.  **Symbol Level (Trading State)**: Durable workflows for `OnPriceUpdate` and `OnOrderUpdate` ensure that trading decisions and exchange side-effects (orders) are executed exactly-once.

### 6.15 Concurrency Patterns & Mutex Minimization

To improve performance and code quality, the system follows these concurrency principles:

1.  **Shared-Nothing Inter-Symbol**: Each `SymbolManager` is strictly isolated. There are no shared locks between BTC and ETH managers.
2.  **Actor-like Dispatch**: The Orchestrator uses a read-only or `sync.Map` lookup for routing. Events are pushed into symbol-specific workers, minimizing global lock contention.
3.  **Atomic Snapshots for Monitoring**: Instead of external components (like `LiveServer`) holding locks on the `PositionManager` to read state, the manager provides a `GetSnapshot()` method. This method captures a consistent state under a brief read-lock and returns a value-copy, allowing the monitor to process data without blocking the engine.
4.  **Channel Buffering**: Strategic use of buffered channels between the gRPC receiver and the `SymbolManager` prevents a single slow symbol from causing back-pressure on the entire connection.

### 6.15 Advanced Risk Management

Phase 18 introduces a dedicated `RiskEngine` that evaluates global state:
- **Aggregate Exposure**: Monitors the sum of positions across all symbols.
- **Circuit Breaker state**: A global state that can halt all order placement.
- **Latency Monitoring**: Monitors gRPC round-trip times and pauses trading if connectivity degrades.

### 6.16 Observability Architecture

#### 6.16.1 Metrics (Prometheus)
The system uses OpenTelemetry to collect and export metrics to Prometheus via the `/metrics` endpoint.
- **Key Metrics**: PnL, Order Counts, Position Size, Volume, Latency.
- **Registry**: Global OTel MeterProvider initialized in `pkg/telemetry`.

#### 6.16.2 Alerting
The `AlertManager` (`internal/alert`) dispatches critical notifications to configured channels.
- **Channels**: Slack (Webhook), Telegram (Bot API).
- **Triggers**: Circuit Breakers, Risk Limits, Connectivity Loss.

#### 6.16.3 Dashboards
Standardized Grafana dashboards (`configs/dashboards/`) visualize key metrics for real-time monitoring.

## 7. Developer Experience & Tooling

### 7.1 Makefile Standard
The `market_maker/` directory includes a `Makefile` that serves as the single entry point for all development tasks:
- `make build`: Compiles all project binaries.
- `make test`: Runs the full test suite with race detection.
- `make audit`: Runs comprehensive quality checks (linting, vulnerability scans).
- `make proto`: Regenerates gRPC code from definitions.

### 7.2 Git Hooks (pre-commit)
The repository uses the `pre-commit` framework to enforce standards at the time of commit. This ensures that no code enters the repository without passing formatting and static analysis gates.
- **Go**: Validated via `golangci-lint` and `go-mod-tidy`.
- **Python**: Validated via `ruff`.
- **General**: YAML validation and whitespace enforcement.

### 7.3 Workspace Maintenance
The `scripts/audit_branches.sh` tool helps developers maintain a clean local environment by identifying branches that have been merged or squashed.
- **Merge Detection**: Identifies branches that are direct ancestors of main.
- **Squash Detection**: Uses tree hash comparison to find branches that were squash-merged.
- **Safety**: Read-only tool that provides a report of 'active', 'merged', or 'squashed' status.

## 8. Future Roadmap
