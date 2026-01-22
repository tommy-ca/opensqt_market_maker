# Phase 15: Standalone Live Server (Revised Architecture)

## Executive Summary

Build a **standalone live_server binary** using proven market_maker building blocks. This provides clean separation of concerns: market_maker focuses on trading, live_server focuses on monitoring/visualization.

## Architecture Decision

### Original Approach (Embedded)
```
market_maker binary
├── Trading Engine
├── Exchange Connectors
└── Live Server (embedded) ← All in one
```

**Pros**: Single deployment, shared state
**Cons**: Coupling, frontend issues could affect trading

### **Revised Approach (Standalone with Shared Libraries)**
```
Shared Libraries (pkg/)
├── Exchange connectors
├── WebSocket utilities
├── Protobuf types
└── Telemetry/logging

market_maker binary          live_server binary
├── Trading Engine           ├── WebSocket Hub
├── Grid Strategy            ├── Frontend Server
├── Risk Management          ├── Event Aggregator
└── Uses: pkg/exchange       └── Uses: pkg/exchange
```

**Pros**: 
- ✅ Complete isolation (frontend can't crash trading)
- ✅ Independent scaling (run multiple live_servers)
- ✅ Easier testing (test each binary separately)
- ✅ Cleaner deployment (choose which to run)
- ✅ Code reuse (shared pkg/ libraries)

**Cons**: 
- Slightly more deployment complexity (2 binaries vs 1)
- Need communication mechanism (Redis/NATS or duplicate connections)

## Comparison Matrix

| Aspect | Embedded Approach | Standalone Approach |
|--------|-------------------|---------------------|
| **Isolation** | Same process | Separate processes ✅ |
| **Deployment** | 1 binary | 2 binaries |
| **Scaling** | Coupled | Independent ✅ |
| **Code Reuse** | Internal only | Shared pkg/ ✅ |
| **Testing** | Complex | Simple ✅ |
| **Resource Usage** | Lower | Slightly higher |
| **Failure Isolation** | Risky | Safe ✅ |
| **Development** | Tightly coupled | Loose coupling ✅ |

## New Architecture

### Directory Structure

```
opensqt_market_maker/
├── cmd/
│   ├── market_maker/          # Trading engine binary
│   │   └── main.go
│   └── live_server/           # NEW: Monitoring binary
│       └── main.go
├── pkg/                       # NEW: Shared libraries
│   ├── exchange/             # Reusable exchange connectors
│   │   ├── binance/
│   │   ├── bitget/
│   │   ├── gate/
│   │   ├── okx/
│   │   ├── bybit/
│   │   └── interface.go
│   ├── liveserver/           # WebSocket hub and server
│   │   ├── hub.go
│   │   ├── messages.go
│   │   ├── server.go
│   │   └── authenticator.go
│   ├── tradingutils/         # Trading math utilities
│   ├── telemetry/            # OpenTelemetry
│   └── logging/              # Structured logging
├── internal/                  # Private to market_maker
│   ├── engine/
│   ├── trading/
│   ├── risk/
│   └── safety/
├── web/                       # Frontend assets
│   └── live.html
└── configs/
    ├── market_maker.yaml
    └── live_server.yaml
```

### Component Responsibilities

#### market_maker Binary
**Purpose**: Execute trading strategies
- Grid strategy logic
- Risk management
- Order execution
- State persistence
- Durable workflows

#### live_server Binary  
**Purpose**: Monitor and visualize exchange data
- WebSocket broadcasting
- Frontend serving
- Exchange data streaming
- Historical data querying
- Multi-client management

#### Shared pkg/ Libraries
**Purpose**: Reusable components
- Exchange connectors (IExchange interface)
- WebSocket hub pattern
- Protobuf message types
- Logging and telemetry
- Configuration parsing

## Communication Options

### Option 1: Duplicate Exchange Connections (Simplest)
Each binary connects to the exchange independently.

**Pros**:
- ✅ Zero coupling
- ✅ Simple implementation
- ✅ Independent failure domains

**Cons**:
- ❌ Double WebSocket connections (minor cost)

### Option 2: Event Bus (Redis/NATS)
market_maker publishes events, live_server subscribes.

**Pros**:
- ✅ Single exchange connection
- ✅ Scalable to multiple live_servers
- ✅ Can add other consumers (metrics, alerts)

**Cons**:
- ❌ Additional infrastructure
- ❌ More complexity

### **Recommended: Option 1 for MVP, migrate to Option 2 later**

## Implementation Plan

### Phase 15A: Extract Shared Libraries (Week 1)

#### Task 15A.1: Extract Exchange Connectors to pkg/
**Goal**: Make exchange connectors reusable

**Steps**:
1. Move `market_maker/internal/exchange/` → `pkg/exchange/`
2. Update imports in market_maker
3. Verify all tests still pass

**Files to Move**:
- `pkg/exchange/interface.go` (IExchange)
- `pkg/exchange/binance/`
- `pkg/exchange/bitget/`
- `pkg/exchange/gate/`
- `pkg/exchange/okx/`
- `pkg/exchange/bybit/`
- `pkg/exchange/factory.go`

**Tests**: 
- [ ] Run all market_maker tests
- [ ] Verify no import cycles
- [ ] Check binary builds

#### Task 15A.2: Create pkg/liveserver Package
**Goal**: Reusable WebSocket hub

**Files to Create**:
```
pkg/liveserver/
├── hub.go           # WebSocket hub pattern
├── hub_test.go
├── messages.go      # Message type definitions
├── messages_test.go
├── server.go        # HTTP + WebSocket server
├── server_test.go
├── auth.go          # Optional authentication
└── auth_test.go
```

**Interface**:
```go
package liveserver

type Hub interface {
    Run(ctx context.Context)
    Register(client *Client)
    Unregister(client *Client)  
    Broadcast(msg Message)
    ClientCount() int
}

type Server interface {
    Start(addr string) error
    Stop(ctx context.Context) error
    GetHub() Hub
}
```

#### Task 15A.3: Extract Common Utilities
**Goal**: Share utilities between binaries

**Move to pkg/**:
- `pkg/logging/` (already exists, verify exports)
- `pkg/telemetry/` (already exists, verify exports)
- `pkg/tradingutils/` (already exists)
- `pkg/pbu/` (protobuf utilities, already exists)

### Phase 15B: Build Standalone live_server Binary (Week 2)

#### Task 15B.1: Create cmd/live_server/main.go
**Goal**: New binary for monitoring

**Structure**:
```go
package main

import (
    "flag"
    "log"
    
    "market_maker/pkg/exchange"
    "market_maker/pkg/liveserver"
    "market_maker/pkg/logging"
)

func main() {
    configFile := flag.String("config", "configs/live_server.yaml", "Config file")
    port := flag.String("port", ":8081", "WebSocket port")
    flag.Parse()
    
    // 1. Initialize logger
    logger, _ := logging.NewZapLogger("INFO")
    
    // 2. Load config
    cfg := loadConfig(*configFile)
    
    // 3. Initialize exchange (read-only)
    exch, err := exchange.NewExchange(cfg.Exchange.Name, cfg, logger)
    if err != nil {
        log.Fatal(err)
    }
    
    // 4. Create live server
    hub := liveserver.NewHub(logger)
    server := liveserver.NewServer(hub, logger)
    
    // 5. Subscribe to exchange streams
    go streamKlines(exch, hub, cfg.Symbol)
    go streamOrders(exch, hub)
    go streamAccount(exch, hub)
    
    // 6. Start HTTP/WebSocket server
    server.Start(*port)
}
```

#### Task 15B.2: Implement Stream Handlers
**Goal**: Convert exchange events to WebSocket messages

**Files**:
```
cmd/live_server/
├── main.go
├── streams.go       # Exchange stream handlers
├── config.go        # Configuration loading
└── handlers.go      # HTTP handlers for frontend
```

**Example**:
```go
// streams.go
func streamKlines(exch pkg.IExchange, hub liveserver.Hub, symbol string) {
    ctx := context.Background()
    
    err := exch.StartKlineStream(ctx, []string{symbol}, "1m", func(candle *pb.Candle) {
        msg := liveserver.Message{
            Type: "kline",
            Data: map[string]interface{}{
                "time":   candle.OpenTime / 1000,
                "open":   candle.Open,
                "high":   candle.High,
                "low":    candle.Low,
                "close":  candle.Close,
                "volume": candle.Volume,
            },
        }
        hub.Broadcast(msg)
    })
    
    if err != nil {
        log.Printf("Error streaming klines: %v", err)
    }
}
```

#### Task 15B.3: Create Configuration
**File**: `configs/live_server.yaml`

```yaml
exchange:
  name: "binance"  # or bitget, gate, okx, bybit
  
binance:
  api_key: "${BINANCE_API_KEY}"
  secret_key: "${BINANCE_SECRET_KEY}"
  
trading:
  symbol: "BTCUSDT"
  
server:
  port: ":8081"
  enable_auth: false
  allowed_origins:
    - "http://localhost:3000"
    - "https://opensqt.com"
    
logging:
  level: "INFO"
  format: "json"
```

### Phase 15C: Frontend Integration (Week 3)

#### Task 15C.1: Serve Frontend Assets
**Goal**: Serve live.html at /live

**Implementation**:
```go
// cmd/live_server/handlers.go
func (s *Server) serveFrontend(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "web/live.html")
}

func (s *Server) setupRoutes() {
    http.HandleFunc("/", s.serveFrontend)
    http.HandleFunc("/ws", s.handleWebSocket)
    http.HandleFunc("/health", s.handleHealth)
}
```

#### Task 15C.2: Historical Data Endpoint
**Goal**: Serve initial candles on connect

**Implementation**:
```go
func sendHistoricalData(client *liveserver.Client, exch pkg.IExchange, symbol string) {
    candles, err := exch.GetHistoricalKlines(context.Background(), symbol, "1m", 100)
    if err != nil {
        log.Printf("Error fetching history: %v", err)
        return
    }
    
    var history []map[string]interface{}
    for _, c := range candles {
        history = append(history, map[string]interface{}{
            "time":   c.OpenTime / 1000,
            "open":   c.Open,
            "high":   c.High,
            "low":    c.Low,
            "close":  c.Close,
            "volume": c.Volume,
        })
    }
    
    client.Send(liveserver.Message{
        Type: "history",
        Data: history,
    })
}
```

#### Task 15C.3: Reuse Existing Frontend
**Goal**: Use live-standalone.html without changes

**Copy**:
```bash
cp live_server/这里面留在自己电脑/live-standalone.html web/live.html
cp live_server/这里面留在自己电脑/coin.mp3 web/coin.mp3
```

**Update**: Change WebSocket URL to be configurable

### Phase 15D: Testing & Documentation (Week 4)

#### Task 15D.1: Unit Tests
```go
// pkg/liveserver/hub_test.go
func TestHubBroadcast(t *testing.T) {
    hub := NewHub(nil)
    client := NewMockClient()
    
    hub.Register(client)
    
    msg := Message{Type: "kline", Data: map[string]interface{}{"price": "42000"}}
    hub.Broadcast(msg)
    
    assert.Equal(t, msg, client.LastMessage)
}
```

#### Task 15D.2: Integration Tests
```go
// cmd/live_server/main_test.go
func TestLiveServerE2E(t *testing.T) {
    // Start server
    go main()
    time.Sleep(100 * time.Millisecond)
    
    // Connect WebSocket
    ws, _, err := websocket.DefaultDialer.Dial("ws://localhost:8081/ws", nil)
    require.NoError(t, err)
    
    // Read message
    var msg liveserver.Message
    err = ws.ReadJSON(&msg)
    require.NoError(t, err)
    
    assert.Equal(t, "history", msg.Type)
}
```

#### Task 15D.3: Documentation
- [ ] README.md for live_server
- [ ] Configuration guide
- [ ] Deployment guide
- [ ] API documentation (message types)

## Deployment

### Development
```bash
# Terminal 1: Run trading engine
cd market_maker
go run cmd/market_maker/main.go --config configs/config.yaml

# Terminal 2: Run live server
cd market_maker
go run cmd/live_server/main.go --config configs/live_server.yaml --port :8081

# Browser: Open http://localhost:8081/live
```

### Production (Docker Compose)
```yaml
version: '3.8'

services:
  market-maker:
    build:
      context: .
      dockerfile: market_maker/Dockerfile
    volumes:
      - ./data:/app/data
      - ./configs/market_maker.yaml:/app/config.yaml
    environment:
      - BINANCE_API_KEY=${BINANCE_API_KEY}
      - BINANCE_SECRET_KEY=${BINANCE_SECRET_KEY}
    ports:
      - "8080:8080"  # Health endpoint
      
  live-server:
    build:
      context: .
      dockerfile: Dockerfile.live_server
    volumes:
      - ./configs/live_server.yaml:/app/config.yaml
    environment:
      - BINANCE_API_KEY=${BINANCE_API_KEY}
      - BINANCE_SECRET_KEY=${BINANCE_SECRET_KEY}
    ports:
      - "8081:8081"  # WebSocket + Frontend
    depends_on:
      - market-maker  # Optional: can run independently
```

### Dockerfile.live_server
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
COPY --from=builder /build/web ./web
COPY --from=builder /build/configs ./configs
EXPOSE 8081
CMD ["./live_server", "--config", "configs/live_server.yaml", "--port", ":8081"]
```

## Benefits of Standalone Approach

### 1. **Complete Isolation**
- Frontend bugs CANNOT crash trading engine
- Each binary has independent lifecycle
- Restart live_server without affecting trades

### 2. **Independent Scaling**
- Run multiple live_servers for different symbols
- Scale monitoring independently from trading
- Deploy live_server without trading logic

### 3. **Cleaner Testing**
- Test trading engine without UI concerns
- Test live_server with mock exchange
- Simpler integration tests

### 4. **Flexible Deployment**
- Run only market_maker (headless trading)
- Run only live_server (monitoring existing positions)
- Run both (full stack)

### 5. **Code Reuse**
- Shared pkg/ libraries benefit both binaries
- Single source of truth for exchange connectors
- Consistent message formats via protobufs

## Migration Path

### From Current live_server
```bash
# Old: Standalone Go files
live_server/biance/main.go (408 lines)
live_server/bitget/main.go (987 lines)

# New: Reuses proven components
cmd/live_server/main.go (~200 lines)
├── Uses pkg/exchange/binance (tested, proven)
├── Uses pkg/exchange/bitget (tested, proven)
├── Uses pkg/liveserver/hub (new, tested)
└── Uses pkg/liveserver/server (new, tested)
```

### Gradual Migration
1. **Week 1**: Extract pkg/ libraries, verify market_maker still works
2. **Week 2**: Build new cmd/live_server, test with Binance
3. **Week 3**: Add remaining exchanges, test with all
4. **Week 4**: Deploy to production, monitor, deprecate old live_server

## Success Criteria

- [ ] New live_server binary builds successfully
- [ ] WebSocket broadcasts work for all exchanges
- [ ] Frontend displays charts and orders correctly
- [ ] < 50ms broadcast latency
- [ ] 100+ concurrent clients supported
- [ ] market_maker binary still works unchanged
- [ ] All existing tests pass
- [ ] New tests achieve >80% coverage
- [ ] Documentation complete

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Duplicate exchange connections | Acceptable for MVP, optimize with event bus later |
| Import cycle between pkg/ and internal/ | Keep internal/ private to market_maker only |
| Breaking changes to IExchange | Semantic versioning, deprecation warnings |
| Frontend compatibility | Keep message format identical to old live_server |

## Timeline

- **Week 1**: Extract shared libraries
- **Week 2**: Build standalone binary
- **Week 3**: Frontend integration
- **Week 4**: Testing and deployment

**Total: 4 weeks** (same as embedded approach, but cleaner architecture)

## Conclusion

The standalone approach provides:
- ✅ Better separation of concerns
- ✅ Complete failure isolation
- ✅ Flexible deployment options
- ✅ Cleaner code reuse via pkg/
- ✅ Independent scaling

This is the **recommended approach** for Phase 15.
