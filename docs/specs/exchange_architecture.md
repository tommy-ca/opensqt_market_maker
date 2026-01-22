# Exchange Connector Architecture - Production Design

**Version**: 2.1  
**Date**: January 22, 2026  
**Status**: PRODUCTION READY  
**Authors**: OpenSQT Team

---

## Executive Summary

This document defines the **authoritative architecture** for exchange connectivity in the OpenSQT Market Maker system. The architecture enforces a **gRPC-first design** where trading binaries communicate with exchanges through a centralized connector service.

### Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                    OpenSQT Market Maker System                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────────┐        ┌─────────────────┐                │
│  │  market_maker   │        │   live_server   │                │
│  │  (Trading)      │        │   (Monitoring)  │                │
│  └────────┬────────┘        └────────┬────────┘                │
│           │                          │                          │
│           │  internal/exchange       │  internal/exchange       │
│           │  RemoteExchange          │  RemoteExchange          │
│           │  (gRPC Client)           │  (gRPC Client)           │
│           │                          │                          │
│           └──────────┬───────────────┘                          │
│                      │ gRPC                                     │
│                      ▼                                          │
│           ┌─────────────────────┐                               │
│           │ exchange_connector  │                               │
│           │ (gRPC Server)       │                               │
│           │                     │                               │
│           │ internal/exchange   │                               │
│           │ ExchangeServer      │                               │
│           └──────────┬──────────┘                               │
│                      │                                          │
│                      │ pkg/exchange.Adapter                     │
│                      ▼                                          │
│           ┌─────────────────────┐                               │
│           │ Native Connectors   │                               │
│           │ internal/exchange/  │                               │
│           │ {binance,bitget,    │                               │
│           │  bybit,gate,okx}    │                               │
│           └──────────┬──────────┘                               │
│                      │                                          │
│                      │ REST/WebSocket                           │
│                      ▼                                          │
│           ┌─────────────────────┐                               │
│           │  Exchange APIs      │                               │
│           │  (Binance, etc.)    │                               │
│           └─────────────────────┘                               │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Key Benefits

| Benefit | Description | Impact |
|---------|-------------|--------|
| **Fault Isolation** | Exchange issues don't crash trading engine | HIGH |
| **Shared Connections** | Single WebSocket shared by all clients | HIGH |
| **Rate Limit Management** | Centralized rate limiting | HIGH |
| **Language Flexibility** | Python connectors via gRPC | MEDIUM |
| **Independent Updates** | Update connectors without restart | MEDIUM |
| **Consistent Behavior** | All clients see identical data | HIGH |

---

## 1. Package Structure

### 1.1 Directory Layout

```
market_maker/
├── cmd/
│   ├── market_maker/           # Trading engine binary
│   │   └── main.go             # Uses RemoteExchange (gRPC client)
│   ├── live_server/            # Monitoring server binary  
│   │   └── main.go             # Uses RemoteExchange (gRPC client)
│   └── exchange_connector/     # gRPC server binary
│       └── main.go             # Wraps native connectors
│
├── internal/
│   ├── core/
│   │   └── interfaces.go       # IExchange, ILogger interfaces
│   ├── exchange/
│   │   ├── remote.go           # RemoteExchange (gRPC client)
│   │   ├── server.go           # ExchangeServer (gRPC server)
│   │   ├── factory.go          # Exchange factory
│   │   ├── binance/            # Native Binance connector
│   │   ├── bitget/             # Native Bitget connector
│   │   ├── bybit/              # Native Bybit connector
│   │   ├── gate/               # Native Gate connector
│   │   └── okx/                # Native OKX connector
│   ├── logging/                # Simple logger implementation
│   ├── pb/                     # Generated protobuf code
│   ├── trading/                # Trading logic
│   └── ...
│
├── pkg/
│   ├── exchange/               # Public Exchange interface + Adapter
│   │   ├── exchange.go         # Exchange interface & Adapter
│   │   └── exchange_test.go    # Unit tests
│   ├── logging/                # ZapLogger with OpenTelemetry
│   ├── pbu/                    # Protobuf utilities
│   └── ...
│
├── api/proto/                  # Protobuf definitions
├── configs/                    # Configuration files
└── docs/specs/                 # Specification documents
```

### 1.2 Key Design Decisions

**Decision 1: Native Connectors Stay in `internal/exchange/`**

Native connectors remain in `internal/` because:
- They are implementation details, not public API
- They depend on `internal/core.ILogger` and `internal/pb`
- The `pkg/exchange.Adapter` provides public API if needed

**Decision 2: `pkg/exchange` Provides Adapter Pattern**

The `pkg/exchange` package provides:
- `Exchange` interface (public API definition)
- `Adapter` type that wraps any `core.IExchange` implementation
- Allows external packages to use exchange functionality without importing `internal/`

**Decision 3: Logger Interface in `internal/core`**

The `ILogger` interface is defined in `internal/core` because:
- All internal packages need consistent logging
- `pkg/logging` implements `core.ILogger` (valid - pkg CAN import internal in same module)
- External consumers would define their own logger interface

---

## 2. Interface Contracts

### 2.1 Internal Exchange Interface (`internal/core/interfaces.go`)

```go
type IExchange interface {
    // Metadata
    GetName() string
    GetType() pb.ExchangeType
    
    // Order Management
    PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error)
    BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool)
    CancelOrder(ctx context.Context, symbol string, orderId int64) error
    GetOrder(ctx context.Context, symbol string, orderId int64) (*pb.Order, error)
    GetOpenOrders(ctx context.Context, symbol string) ([]*pb.Order, error)
    
    // Account & Positions
    GetAccount(ctx context.Context) (*pb.Account, error)
    GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error)
    GetBalance(ctx context.Context, asset string) (decimal.Decimal, error)
    
    // WebSocket Streams
    StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error
    StartPriceStream(ctx context.Context, symbol string, callback func(change *pb.PriceChange)) error
    StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error
    StartAccountStream(ctx context.Context, callback func(account *pb.Account)) error
    StartPositionStream(ctx context.Context, callback func(position *pb.Position)) error
    StopOrderStream() error
    StopKlineStream() error
    
    // Market Data
    GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error)
    GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error)
    FetchExchangeInfo(ctx context.Context, symbol string) error
    GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error)
    
    // Contract Info
    GetPriceDecimals() int
    GetQuantityDecimals() int
    GetBaseAsset() string
    GetQuoteAsset() string
}
```

### 2.2 Public Exchange Interface (`pkg/exchange/exchange.go`)

```go
type Exchange interface {
    // Same methods as core.IExchange
    // Plus: CheckHealth(ctx context.Context) error
}

// Adapter wraps core.IExchange to implement pkg.Exchange
type Adapter struct {
    impl interface{ ... }
}

func NewAdapter(impl interface{}) Exchange
```

### 2.3 Logger Interface (`internal/core/interfaces.go`)

```go
type ILogger interface {
    Debug(msg string, fields ...interface{})
    Info(msg string, fields ...interface{})
    Warn(msg string, fields ...interface{})
    Error(msg string, fields ...interface{})
    Fatal(msg string, fields ...interface{})
    WithField(key string, value interface{}) ILogger
    WithFields(fields map[string]interface{}) ILogger
}
```

---

## 3. Data Flow

### 3.1 Order Placement Flow

```
market_maker                    exchange_connector              Exchange API
     │                                │                              │
     │ PlaceOrder(req)                │                              │
     ├────────────────────────────────►                              │
     │                                │                              │
     │         gRPC PlaceOrder        │                              │
     │ ◄──────────────────────────────┤                              │
     │                                │                              │
     │                                │ REST POST /order             │
     │                                ├──────────────────────────────►
     │                                │                              │
     │                                │ Order Response               │
     │                                ◄──────────────────────────────┤
     │                                │                              │
     │ *pb.Order                      │                              │
     ◄────────────────────────────────┤                              │
     │                                │                              │
```

### 3.2 WebSocket Stream Flow

```
market_maker                    exchange_connector              Exchange WS
     │                                │                              │
     │ StartOrderStream(callback)     │                              │
     ├────────────────────────────────►                              │
     │                                │                              │
     │ gRPC SubscribeOrders           │                              │
     │ ◄──────────────────────────────┤                              │
     │                                │                              │
     │                                │ WebSocket Connect            │
     │                                ├──────────────────────────────►
     │                                │                              │
     │                    ┌───────────┼──────────────────────────────┤
     │                    │ Loop      │                              │
     │                    │           │ Order Update                 │
     │                    │           ◄──────────────────────────────┤
     │                    │           │                              │
     │ callback(update)   │           │                              │
     ◄────────────────────┼───────────┤                              │
     │                    │           │                              │
     │                    └───────────┼──────────────────────────────┤
     │                                │                              │
```

---

## 4. Configuration

### 4.1 market_maker Configuration (`configs/config.yaml`)

```yaml
app:
  # PRODUCTION: Use "remote" to connect via gRPC
  # DEVELOPMENT: Use exchange name directly (binance, etc.)
  current_exchange: "remote"

exchanges:
  # gRPC Exchange Connector (RECOMMENDED for production)
  remote:
    base_url: "localhost:50051"
  
  # Native connectors (development/testing only)
  binance:
    api_key: "YOUR_KEY"
    secret_key: "YOUR_SECRET"
    fee_rate: 0.0002
```

### 4.2 exchange_connector Environment Variables

```bash
EXCHANGE=binance          # Which native connector to use
API_KEY=xxx               # Exchange API key
API_SECRET=yyy            # Exchange API secret
PASSPHRASE=zzz            # OKX only
PORT=50051                # gRPC listen port
LOG_LEVEL=INFO            # Logging level
```

### 4.3 Docker Compose (`docker-compose.grpc.yml`)

```yaml
services:
  exchange_connector:
    build: ./market_maker
    environment:
      - EXCHANGE=binance
      - API_KEY=${API_KEY}
      - API_SECRET=${API_SECRET}
    ports:
      - "50051:50051"
    healthcheck:
      test: ["CMD", "grpc_health_probe", "-addr=:50051"]
  
  market_maker:
    build: ./market_maker
    depends_on:
      exchange_connector:
        condition: service_healthy
    environment:
      - CURRENT_EXCHANGE=remote
      - EXCHANGE_GRPC_ADDRESS=exchange_connector:50051
  
  live_server:
    build: ./market_maker
    depends_on:
      exchange_connector:
        condition: service_healthy
    environment:
      - EXCHANGE_TYPE=remote
      - EXCHANGE_GRPC_ADDRESS=exchange_connector:50051
    ports:
      - "8081:8081"
```

---

## 5. Production Deployment

### 5.1 Recommended Architecture

```
                    ┌─────────────────────────────────────┐
                    │         Kubernetes Cluster          │
                    │                                     │
                    │  ┌───────────────────────────────┐  │
                    │  │     exchange_connector        │  │
                    │  │     (1 replica)               │  │
                    │  │     Port: 50051               │  │
                    │  └───────────────┬───────────────┘  │
                    │                  │                  │
                    │         ┌────────┴────────┐         │
                    │         │                 │         │
                    │  ┌──────▼──────┐  ┌───────▼──────┐  │
                    │  │market_maker │  │ live_server  │  │
                    │  │(1 replica)  │  │(1-3 replicas)│  │
                    │  └─────────────┘  └──────────────┘  │
                    │                                     │
                    └─────────────────────────────────────┘
```

### 5.2 Deployment Checklist

- [ ] Exchange connector is single instance (shared connection)
- [ ] Trading binaries use `current_exchange: remote`
- [ ] Health checks configured
- [ ] Credentials in secrets (not config files)
- [ ] gRPC port NOT exposed externally
- [ ] Logging level appropriate for production
- [ ] Resource limits configured

### 5.3 Health Checks

```bash
# Check exchange_connector health
grpc_health_probe -addr=localhost:50051

# Check market_maker health
curl http://localhost:8080/health

# Check live_server health  
curl http://localhost:8081/health
```

---

## 6. Development Workflow

### 6.1 Local Development (Native Connectors)

For development/testing, use native connectors directly:

```yaml
# configs/config.yaml
app:
  current_exchange: "binance"  # Direct native connector
```

```bash
# Run market_maker with native connector
./bin/market_maker --config configs/config.yaml
```

### 6.2 Local Development (gRPC Stack)

To test gRPC architecture locally:

```bash
# Terminal 1: Start exchange_connector
EXCHANGE=binance API_KEY=xxx API_SECRET=yyy ./bin/exchange_connector

# Terminal 2: Start market_maker with remote
./bin/market_maker --config configs/config.yaml  # current_exchange: remote

# Terminal 3: Start live_server
./bin/live_server --config configs/live_server.yaml
```

### 6.3 Mock Exchange (Testing)

For unit tests and CI:

```yaml
# configs/config.yaml
app:
  current_exchange: "mock"
```

---

## 7. Testing

### 7.1 Unit Tests

```bash
# Run all unit tests
go test ./...

# Run exchange-specific tests
go test ./internal/exchange/binance -v
go test ./internal/exchange/bitget -v
```

### 7.2 Integration Tests

```bash
# Run E2E tests
go test ./tests/e2e -v

# Run gRPC integration tests
go test ./internal/exchange -v -run Integration
```

### 7.3 Test Coverage

Target: 80% coverage for core packages

```bash
go test ./internal/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

---

## 8. Monitoring & Observability

### 8.1 Metrics (Prometheus)

- `exchange_connector_requests_total` - Total gRPC requests
- `exchange_connector_request_duration_seconds` - Request latency
- `exchange_connector_errors_total` - Error count
- `exchange_websocket_messages_total` - WebSocket message count

### 8.2 Logging

All components use structured logging:

```json
{
  "timestamp": "2026-01-22T10:30:00Z",
  "level": "INFO",
  "component": "exchange_connector",
  "exchange": "binance",
  "msg": "Order placed",
  "order_id": 12345,
  "latency_ms": 45
}
```

### 8.3 Tracing (OpenTelemetry)

- Distributed tracing across gRPC calls
- Trace context propagation
- Jaeger/Zipkin export supported

---

## 9. Error Handling

### 9.1 gRPC Status Codes

| Scenario | gRPC Code | Action |
|----------|-----------|--------|
| Exchange unreachable | `UNAVAILABLE` | Retry with backoff |
| Invalid order | `INVALID_ARGUMENT` | Return error to caller |
| Rate limited | `RESOURCE_EXHAUSTED` | Retry after delay |
| Authentication failed | `UNAUTHENTICATED` | Check credentials |
| Server error | `INTERNAL` | Log and alert |

### 9.2 Reconnection Strategy

```
Attempt 1: Immediate retry
Attempt 2: Wait 1s, retry
Attempt 3: Wait 2s, retry
Attempt 4: Wait 4s, retry
...
Max backoff: 60s
Max attempts: 10 (then fail-safe exit)
```

---

## 10. Security

### 10.1 Credential Management

- Exchange API keys stored in environment variables
- Never commit credentials to git
- Use Kubernetes secrets in production
- `market_maker` and `live_server` have NO access to exchange credentials

### 10.2 Network Security

- gRPC connections on internal network only
- TLS for production (future enhancement)
- No external exposure of port 50051

---

## 11. Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2026-01-20 | Initial gRPC architecture (Phase 16) |
| 2.0 | 2026-01-22 | Clean separation proposal |
| 2.1 | 2026-01-22 | Production-ready design (current) |

---

## 12. References

- [`grpc_architecture_requirements.md`](./grpc_architecture_requirements.md) - Detailed requirements
- [`architecture_audit_jan2026.md`](./architecture_audit_jan2026.md) - Audit report
- [`phase16_test_spec.md`](./phase16_test_spec.md) - Test specifications
- [`plan.md`](./plan.md) - Implementation tracking
