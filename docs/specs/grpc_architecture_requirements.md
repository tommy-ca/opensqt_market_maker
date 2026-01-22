# gRPC Architecture Requirements (Phase 16)
**Version**: 1.0  
**Date**: January 22, 2026  
**Status**: MANDATORY for Production

---

## 1. Executive Summary

### 1.1 Architectural Mandate

**ALL production deployments of OpenSQT Market Maker MUST use the gRPC-based architecture.**

Both `market_maker` and `live_server` binaries must communicate with exchanges exclusively through the `exchange_connector` gRPC service. Direct native connector usage is deprecated and violates the documented architecture.

### 1.2 Benefits

| Benefit | Description | Impact |
|---------|-------------|--------|
| **Fault Isolation** | Exchange connection issues don't crash trading engine | HIGH |
| **Shared Connections** | Single WebSocket connection shared by both binaries | HIGH |
| **Rate Limit Management** | Centralized rate limiting across all clients | HIGH |
| **Language Flexibility** | Python connectors work seamlessly with Go clients | MEDIUM |
| **Independent Updates** | Update exchange adapters without restarting trading | MEDIUM |
| **Better Monitoring** | Single process to monitor for exchange health | MEDIUM |
| **Consistent Behavior** | Both binaries see identical exchange data | HIGH |

---

## 2. Component Requirements

### 2.1 exchange_connector Service (gRPC Server)

#### 2.1.1 REQ-GRPC-001: Service Deployment
- **MUST** run as a separate process/container
- **MUST** expose gRPC service on configurable port (default: 50051)
- **MUST** support graceful shutdown (SIGTERM/SIGINT)
- **MUST** implement health checks via `grpc.health.v1.Health`

#### 2.1.2 REQ-GRPC-002: Exchange Adapter Management
- **MUST** wrap ONE native exchange connector (Binance, Bitget, Gate, OKX, or Bybit)
- **MUST** initialize exchange connection on startup
- **MUST** validate exchange credentials before serving
- **MUST** handle exchange disconnections and reconnect automatically

#### 2.1.3 REQ-GRPC-003: gRPC Service Implementation
- **MUST** implement all RPCs defined in `ExchangeService` proto
- **MUST** support server-side streaming for:
  - `SubscribePrice` - Real-time price updates
  - `SubscribeOrders` - Order execution reports
  - `SubscribeKlines` - Candlestick data
  - `SubscribeAccount` - Account balance updates (NEW)
  - `SubscribePositions` - Position updates (NEW)
- **MUST** support unary RPCs for all order operations and queries

#### 2.1.4 REQ-GRPC-004: Stream Management
- **MUST** cancel underlying exchange streams when gRPC client disconnects
- **MUST** handle multiple concurrent stream subscriptions
- **MUST** buffer messages to prevent slow clients from blocking
- **MUST** use context cancellation for cleanup

#### 2.1.5 REQ-GRPC-005: Error Handling
- **MUST** convert exchange-specific errors to gRPC status codes
- **MUST** include error details in gRPC status messages
- **MUST** log all errors with structured logging

### 2.2 market_maker Client (Trading Engine)

#### 2.2.1 REQ-GRPC-010: Connection Management
- **MUST** connect to exchange_connector via gRPC on startup
- **MUST** support configurable gRPC endpoint (env var or config file)
- **MUST** implement connection retry with exponential backoff
- **MUST** fail fast if exchange_connector unavailable

#### 2.2.2 REQ-GRPC-011: Client Configuration
- **MUST** default to `current_exchange: remote` in production config
- **MUST** read gRPC address from `exchanges.remote.base_url`
- **MUST** support TLS for production deployments (future)
- **MUST NOT** use native connectors in production

#### 2.2.3 REQ-GRPC-012: Stream Consumption
- **MUST** subscribe to order updates via `SubscribeOrders`
- **MUST** subscribe to price updates via `SubscribePrice`
- **MUST** handle stream disconnections gracefully
- **MUST** re-subscribe on reconnection

### 2.3 live_server Client (Monitoring)

#### 2.3.1 REQ-GRPC-020: Connection Management
- **MUST** connect to exchange_connector via gRPC on startup
- **MUST** support configurable gRPC endpoint
- **MUST** operate independently of market_maker
- **MUST** handle exchange_connector restarts

#### 2.3.2 REQ-GRPC-021: Stream Subscriptions
- **MUST** subscribe to klines via `SubscribeKlines`
- **MUST** subscribe to orders via `SubscribeOrders`
- **MUST** subscribe to account via `SubscribeAccount` (NEW)
- **MUST** subscribe to positions via `SubscribePositions` (NEW)
- **MUST** convert gRPC streams to WebSocket messages

#### 2.3.3 REQ-GRPC-022: Read-Only Operations
- **MUST NOT** place or cancel orders (monitoring only)
- **MUST** fetch historical data via `GetHistoricalKlines`
- **MUST** query current positions via `GetPositions`

---

## 3. Protocol Buffer Requirements

### 3.1 REQ-PROTO-001: Service Definition
- **MUST** maintain `ExchangeService` in `market_maker/api/proto/opensqt/market_maker/v1/exchange.proto`
- **MUST** use `buf` CLI for all proto management
- **MUST** use `buf lint` to enforce style and structural standards
- **MUST** use `buf breaking` to detect backward incompatibility
- **MUST** version all breaking changes

### 3.2 REQ-PROTO-002: New Streaming RPCs (Phase 16)

```protobuf
service ExchangeService {
    // Existing RPCs...
    
    // NEW: Account balance streaming (Phase 16)
    rpc SubscribeAccount(SubscribeAccountRequest) returns (stream Account);
    
    // NEW: Position updates streaming (Phase 16)
    rpc SubscribePositions(SubscribePositionsRequest) returns (stream Position);
}

message SubscribeAccountRequest {}

message SubscribePositionsRequest {
    string symbol = 1;  // Optional: filter by symbol
}
```

### 3.3 REQ-PROTO-003: Message Compatibility
- **MUST** maintain backward compatibility for all messages
- **MUST** use optional fields for new additions
- **MUST** document field usage in proto comments

---

## 4. Deployment Requirements

### 4.1 REQ-DEPLOY-001: Multi-Process Architecture

```
┌─────────────────┐
│ exchange_connector │ ← Single process managing exchange connection
└────────┬────────┘
         │ gRPC (port 50051)
    ┌────┴────┐
    │         │
┌───▼──┐   ┌─▼────────┐
│ market │   │ live_server │
│ _maker │   │            │
└───────┘   └─────────┘
```

- **MUST** deploy exchange_connector as separate container/process
- **MUST** deploy market_maker as separate container/process
- **MUST** deploy live_server as separate container/process (optional)
- **MUST** use Docker Compose or Kubernetes for orchestration

### 4.2 REQ-DEPLOY-002: Container Configuration

**exchange_connector**:
```yaml
environment:
  - EXCHANGE=binance  # Exchange name
  - API_KEY=${BINANCE_API_KEY}
  - API_SECRET=${BINANCE_API_SECRET}
  - PORT=50051  # gRPC port
ports:
  - "50051:50051"
```

**market_maker**:
```yaml
environment:
  - CURRENT_EXCHANGE=remote
  - EXCHANGE_GRPC_ADDRESS=exchange_connector:50051
depends_on:
  - exchange_connector
```

**live_server**:
```yaml
environment:
  - EXCHANGE_TYPE=remote
  - EXCHANGE_GRPC_ADDRESS=exchange_connector:50051
depends_on:
  - exchange_connector
ports:
  - "8081:8081"
```

### 4.3 REQ-DEPLOY-003: Health Checks

- **MUST** implement health check endpoint in exchange_connector
- **MUST** configure Docker health check for exchange_connector
- **MUST** configure readiness probes for market_maker
- **MUST** restart exchange_connector on health check failure

### 4.4 REQ-DEPLOY-004: Network Configuration

- **MUST** use Docker network for inter-container communication
- **MUST NOT** expose gRPC port externally (internal only)
- **MUST** use TLS for external gRPC connections (future)

---

## 5. Migration Requirements

### 5.1 REQ-MIGRATE-001: Configuration Migration

**From** (Native - DEPRECATED):
```yaml
app:
  current_exchange: binance  # ❌ Direct native connector

exchanges:
  binance:
    api_key: xxx
    api_secret: yyy
```

**To** (gRPC - REQUIRED):
```yaml
app:
  current_exchange: remote  # ✅ gRPC client

exchanges:
  remote:
    base_url: "exchange_connector:50051"
    # No API keys - exchange_connector handles authentication
```

### 5.2 REQ-MIGRATE-002: Backward Compatibility

- **MUST** support native connectors for development/testing
- **MUST** provide clear deprecation warnings
- **MUST** document migration path in README
- **MUST** provide migration script (future)

### 5.3 REQ-MIGRATE-003: Testing

- **MUST** test all binaries with gRPC before production
- **MUST** verify stream subscriptions work correctly
- **MUST** measure gRPC overhead (latency benchmarks)
- **MUST** validate error handling across gRPC boundary

---

## 6. Performance Requirements

### 6.1 REQ-PERF-001: Latency

- **MUST** maintain total latency < 10ms (exchange → gRPC → client)
- **SHOULD** achieve gRPC overhead < 2ms (99th percentile)
- **MUST** measure and log latency in production

### 6.2 REQ-PERF-002: Throughput

- **MUST** support 1000+ messages/second per stream
- **MUST** handle concurrent streams without degradation
- **MUST** buffer messages to prevent blocking

### 6.3 REQ-PERF-003: Resource Usage

- **MUST** limit exchange_connector memory < 500MB
- **MUST** limit CPU usage < 50% under normal load
- **MUST** avoid memory leaks (24h stability test)

---

## 7. Security Requirements

### 7.1 REQ-SEC-001: Credential Management

- **MUST** store exchange API keys in exchange_connector only
- **MUST NOT** expose API keys to client binaries
- **MUST** use environment variables for secrets
- **MUST** support secret rotation without restart

### 7.2 REQ-SEC-002: Authentication (Future)

- **SHOULD** support gRPC authentication (mTLS or token-based)
- **SHOULD** validate client identity
- **SHOULD** audit all client connections

### 7.3 REQ-SEC-003: Authorization

- **MUST** restrict live_server to read-only operations
- **MUST** allow market_maker full order management
- **MUST** validate operation permissions at gRPC layer

---

## 8. Monitoring Requirements

### 8.1 REQ-MON-001: Structured Logging

- **MUST** log all gRPC requests with:
  - Client ID
  - Method name
  - Timestamp
  - Latency
  - Status code
- **MUST** use JSON format for production logs
- **MUST** include correlation IDs

### 8.2 REQ-MON-002: Metrics

- **MUST** expose Prometheus metrics (future):
  - `grpc_requests_total{method, status}`
  - `grpc_request_duration_seconds{method}`
  - `exchange_stream_connected{exchange}`
  - `exchange_stream_messages_total{stream_type}`

### 8.3 REQ-MON-003: Health Checks

- **MUST** implement `grpc.health.v1.Health/Check`
- **MUST** report SERVING when exchange connected
- **MUST** report NOT_SERVING when exchange disconnected
- **MUST** include exchange health in status response

---

## 9. Implementation Status

### 9.1 Completed (Phase 16.1 - Proto & Server)

- ✅ `SubscribeAccount` RPC added to proto
- ✅ `SubscribePositions` RPC added to proto
- ✅ Server-side streaming methods implemented
- ✅ Proto regenerated with `buf generate`

### 9.2 In Progress (Phase 16.2 - Client)

- 🚧 RemoteExchange client methods
- 🚧 Native exchange stub implementations
- 🚧 Configuration updates

### 9.3 Pending (Phase 16.3 - Deployment)

- ⏳ Docker Compose updates
- ⏳ Dockerfiles for all binaries
- ⏳ Integration testing
- ⏳ Performance benchmarks
- ⏳ Documentation updates

---

## 10. Compliance Matrix

| Requirement | Status | Owner | Priority |
|-------------|--------|-------|----------|
| REQ-GRPC-001 | ✅ Complete | exchange_connector | 🔴 CRITICAL |
| REQ-GRPC-002 | ✅ Complete | exchange_connector | 🔴 CRITICAL |
| REQ-GRPC-003 | 🚧 In Progress | exchange_connector | 🔴 CRITICAL |
| REQ-GRPC-010 | ⏳ Pending | market_maker | 🔴 CRITICAL |
| REQ-GRPC-020 | ⏳ Pending | live_server | 🔴 CRITICAL |
| REQ-PROTO-001 | ✅ Complete | All | 🔴 CRITICAL |
| REQ-PROTO-002 | ✅ Complete | All | 🔴 CRITICAL |
| REQ-DEPLOY-001 | ⏳ Pending | DevOps | 🟡 HIGH |
| REQ-DEPLOY-002 | ⏳ Pending | DevOps | 🟡 HIGH |
| REQ-MIGRATE-001 | ⏳ Pending | All | 🔴 CRITICAL |
| REQ-PERF-001 | ⏳ Pending | Testing | 🟡 HIGH |
| REQ-SEC-001 | ✅ Complete | exchange_connector | 🔴 CRITICAL |
| REQ-MON-001 | ✅ Complete | All | 🟡 HIGH |

**Overall Compliance**: 30% (proto complete, implementation in progress)

---

## 11. References

- **Architectural Audit**: `docs/specs/architecture_audit_jan2026.md`
- **Design Document**: `docs/specs/design_phase15_actual.md` (Section 6.12)
- **Implementation Plan**: `docs/specs/plan.md` (Phase 16)
- **Proto Definition**: `api/proto/opensqt/market_maker/v1/exchange.proto`

---

**Document Owner**: Tech Lead  
**Last Updated**: January 22, 2026  
**Next Review**: After Phase 16 completion
