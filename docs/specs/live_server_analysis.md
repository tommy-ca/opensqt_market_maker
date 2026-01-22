# Live Server Analysis & Feature Requirements

## Overview

The **live_server** is a lightweight WebSocket server that provides real-time exchange data streaming to a frontend visualization client. It supports Binance and Bitget exchanges and serves as a **monitoring and visualization layer** separate from the trading engine.

## Architecture

### Current Structure
```
live_server/
├── biance/main.go     (408 lines - Binance Futures WebSocket server)
├── bitget/main.go     (987 lines - Bitget Futures WebSocket server)  
└── 这里面留在自己电脑/   (Frontend HTML + TradingView integration)
    ├── live-standalone.html  (Charting UI)
    └── coin.mp3             (Trade notification sound)
```

### Components

#### 1. **WebSocket Hub Pattern**
- **Client Management**: Register/unregister WebSocket clients
- **Broadcast System**: Fan-out pattern for broadcasting updates to all connected clients
- **Thread-safe**: Mutex-protected client map with concurrent write handling

#### 2. **Data Streams**

**Public Data (Both Exchanges)**:
- **K-line/Candlestick** (1-minute interval)
  - OHLCV data
  - Real-time price updates
  - Historical candles (100 bars on connect)

**Private Data (Authenticated)**:
- **Account Balance**
  - Available balance
  - Margin balance
  - Account equity
  - Unrealized PnL
  
- **Open Orders**
  - Order ID, price, side, status
  - Order type (limit, market, etc.)
  - Initial snapshot on connect
  - Real-time updates on changes

- **Order Execution Events**
  - Fill notifications
  - Trade side and quantity
  - Execution price
  - Timestamp

- **Positions** (Futures specific)
  - Position size
  - Entry price
  - Unrealized PnL
  - Leverage
  - Position side (long/short)

#### 3. **Message Types**

**Outbound (Server → Frontend)**:
```go
type WSMessage struct {
    Type string      // "kline", "account", "orders", "trade_event", "position", "history"
    Data interface{} // Type-specific payload
}
```

1. **kline**: Real-time 1m candlestick updates
2. **account**: Balance and equity updates  
3. **orders**: Order status changes (NEW, FILLED, CANCELED, etc.)
4. **trade_event**: Trade execution notifications
5. **position**: Futures position updates
6. **history**: Initial historical candles (100 bars)

#### 4. **Exchange-Specific Implementations**

**Binance (biance/main.go)**:
- Uses `github.com/adshao/go-binance/v2/futures`
- **User Stream**: ListenKey-based WebSocket with 30-min keepalive
- **Event Types**:
  - `OrderTradeUpdate`: Order status changes
  - `AccountUpdate`: Balance and position updates
- **Direct WebSocket**: Binance SDK handles reconnection

**Bitget (bitget/main.go)**:
- Custom WebSocket implementation
- **Authentication**: HMAC-SHA256 signature with timestamp
- **Channels**:
  - Public: `candle1m` (K-lines)
  - Private: `orders`, `account`, `positions`
- **Reconnection**: Manual reconnect loop with 3-5s backoff
- **Ping/Pong**: 20s heartbeat interval
- **REST API**: Fallback for account polling (5s interval)

#### 5. **Frontend Integration**

- **TradingView Lightweight Charts**: Professional candlestick charting
- **Real-time Order Lines**: Visual price markers for open orders
- **Trade Notifications**: Sound effects + visual alerts on fills
- **Live Metrics**:
  - Current price
  - Balance
  - Unrealized PnL
  - Open orders count
  - Position size

## Feature Comparison: live_server vs market_maker

| Feature | live_server | market_maker | Gap |
|---------|-------------|--------------|-----|
| **Architecture** | Standalone WebSocket server | Modular trading engine with gRPC | Different purposes |
| **Purpose** | Real-time data visualization | Automated grid trading execution | Complementary |
| **Exchange Support** | Binance, Bitget | Binance, Bitget, Gate, OKX, Bybit | market_maker has more |
| **Data Streams** | ✅ K-lines, Orders, Account, Positions | ✅ All streams via IExchange interface | Parity |
| **WebSocket Management** | ✅ Hub pattern | ✅ Per-exchange implementation | Parity |
| **Reconnection** | ✅ Manual loops | ✅ Context-based lifecycle | market_maker more robust |
| **Frontend API** | ✅ WebSocket JSON | ❌ None (CLI only) | **GAP: No web UI** |
| **Historical Data** | ✅ 100 candles on connect | ✅ `GetHistoricalKlines` | Parity |
| **Authentication** | ✅ API key based | ✅ Config-driven per exchange | Parity |
| **Health Monitoring** | ❌ None | ✅ HTTP endpoints + gRPC health | market_maker superior |
| **State Persistence** | ❌ None (stateless) | ✅ SQLite/Postgres | market_maker superior |
| **Order Execution** | ❌ Read-only | ✅ Full order lifecycle | market_maker only |
| **Risk Management** | ❌ None | ✅ Monitor, Cleaner, Reconciler | market_maker only |
| **Strategy Logic** | ❌ None | ✅ GridStrategy + extensible | market_maker only |
| **Durable Workflows** | ❌ None | ✅ DBOS engine | market_maker only |
| **Multi-client Support** | ✅ Broadcast to multiple browsers | N/A | live_server only |
| **Visual Charting** | ✅ TradingView integration | ❌ None | **GAP: No UI** |
| **Trade Alerts** | ✅ Sound + visual | ❌ Logs only | **GAP: No notifications** |

## Key Insights

### What live_server Does Well
1. **Simple, focused design**: Single-purpose WebSocket broadcaster
2. **Frontend-friendly**: JSON messages optimized for web clients
3. **Real-time visualization**: Seamless TradingView integration
4. **Multi-client**: Broadcast pattern supports multiple viewers
5. **Standalone**: No dependencies on trading engine

### What market_maker Does Better
1. **Production-grade**: Health checks, monitoring, durability
2. **Multi-exchange**: More exchange adapters
3. **Robust lifecycle**: Context-based cancellation, graceful shutdown
4. **State management**: Persistent grid state across restarts
5. **Comprehensive testing**: E2E test suite

### Critical Gaps in market_maker

#### 1. **No Web UI / Visualization**
- live_server provides real-time charting and monitoring
- market_maker is CLI-only
- **Business Impact**: Operators can't monitor strategy performance visually

#### 2. **No Real-time Notifications**
- live_server has trade alerts (sound + visual)
- market_maker only logs to stdout
- **Business Impact**: No awareness of critical events (fills, errors)

#### 3. **No Multi-client Broadcasting**
- live_server uses hub pattern for multiple viewers
- market_maker is single-instance
- **Business Impact**: Can't share live data with team/dashboard

## Integration Opportunities

### Option 1: Standalone Co-deployment
Keep live_server separate, run both services:
- **market_maker**: Executes trading strategy
- **live_server**: Monitors and visualizes
- **Pros**: Simple, no code changes
- **Cons**: Duplicate exchange connections, no integration

### Option 2: Embedded WebSocket Server
Add live_server functionality to market_maker:
- New package: `market_maker/internal/liveserver`
- Reuse existing exchange streams
- Broadcast state updates from engine
- **Pros**: Single deployment, shared state
- **Cons**: Increased complexity

### Option 3: Event Bus Architecture
Decouple via message queue (Redis, NATS):
- **market_maker**: Publishes events (orders, fills, positions)
- **live_server**: Subscribes and broadcasts to WebSocket clients
- **Pros**: Scalable, decoupled
- **Cons**: Additional infrastructure

## Recommended Approach

**Phase 15A: Hybrid Integration**

1. **Extract live_server as reusable module**
   - Create `pkg/liveserver` package
   - Generic `Hub` and `Broadcaster` interfaces
   - Exchange-agnostic message format

2. **Integrate with market_maker engine**
   - Add optional `--live-port` flag
   - Reuse `IPriceMonitor` and `IExchange` streams
   - Broadcast engine state changes (orders, fills, positions)

3. **Extend message types**
   - Add `grid_state` message (current slots, anchor price)
   - Add `risk_alert` message (flash crash, volatility spike)
   - Add `reconciliation` message (ghost orders, state fixes)

4. **Maintain standalone mode**
   - Keep existing `live_server/` for development
   - Use same codebase via Go modules

## Implementation Phases

### Phase 15: Live Server Integration

#### Phase 15A: Extract Core Abstractions
- [ ] Create `pkg/liveserver/hub.go` (generic broadcaster)
- [ ] Create `pkg/liveserver/messages.go` (message types)
- [ ] Create `pkg/liveserver/server.go` (HTTP + WebSocket handler)
- [ ] Unit tests for hub and message serialization

#### Phase 15B: Integrate with market_maker
- [ ] Add `LiveServer` component to `cmd/market_maker/main.go`
- [ ] Subscribe to `IPriceMonitor` and broadcast klines
- [ ] Subscribe to `IOrderExecutor` and broadcast order updates
- [ ] Subscribe to `IPositionManager` and broadcast grid state
- [ ] Add `/live` endpoint (serves `live-standalone.html`)

#### Phase 15C: Advanced Features
- [ ] Add authentication (JWT or API key)
- [ ] Add rate limiting (per client)
- [ ] Add historical replay (from SQLite store)
- [ ] Add multi-symbol support
- [ ] Metrics endpoint for Grafana

#### Phase 15D: Frontend Enhancements
- [ ] Add grid visualization (buy/sell levels)
- [ ] Add risk monitor status indicator
- [ ] Add reconciliation event log
- [ ] Add manual intervention controls (pause/resume)

## Updated Requirements

### Functional Requirements (FR)

**FR-15.1**: Live WebSocket Server
- Market maker MUST expose a WebSocket endpoint for real-time data streaming
- Endpoint MUST support multiple concurrent clients

**FR-15.2**: Real-time Data Broadcast
- System MUST broadcast K-line updates
- System MUST broadcast order status changes
- System MUST broadcast account balance updates
- System MUST broadcast position changes
- System MUST broadcast grid state changes

**FR-15.3**: Historical Data on Connect
- New clients MUST receive last 100 candlesticks
- New clients MUST receive current open orders
- New clients MUST receive current grid state

**FR-15.4**: Message Format
- All messages MUST follow JSON structure: `{"type": string, "data": object}`
- Message types MUST be documented

**FR-15.5**: Frontend Integration
- System MUST serve a standalone HTML monitoring page
- Page MUST render TradingView charts
- Page MUST display real-time metrics
- Page MUST show visual alerts for trades

### Non-Functional Requirements (NFR)

**NFR-15.1**: Performance
- WebSocket broadcast latency MUST be < 50ms
- Server MUST handle 100+ concurrent clients

**NFR-15.2**: Reliability
- Server MUST auto-reconnect exchange streams
- Client disconnects MUST NOT affect trading engine

**NFR-15.3**: Security
- WebSocket endpoint SHOULD support authentication
- Sensitive data (API keys) MUST NOT be transmitted

## Success Criteria

### Phase 15A Success
- [ ] Hub pattern implemented and tested
- [ ] Message serialization 100% coverage
- [ ] Standalone server runs independently

### Phase 15B Success
- [ ] market_maker binary serves `/live` endpoint
- [ ] Real-time K-lines visible in TradingView chart
- [ ] Order events trigger visual updates
- [ ] Grid state displayed with buy/sell levels

### Phase 15C Success
- [ ] Authentication prevents unauthorized access
- [ ] Historical replay works from SQLite
- [ ] Multi-symbol switching works

### Phase 15D Success
- [ ] Grid visualization matches strategy logic
- [ ] Risk alerts display in real-time
- [ ] Manual controls (pause/resume) implemented

## Migration Path

### Week 1: Extraction
- Extract hub + broadcaster from live_server
- Create pkg/liveserver module
- Write unit tests

### Week 2: Integration
- Wire into market_maker engine
- Test with Binance connector
- Validate message flow

### Week 3: Enhancement
- Add grid state broadcasting
- Add risk alert messages
- Update frontend

### Week 4: Production
- Add authentication
- Load testing
- Documentation
- Deploy

## Risks & Mitigations

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| WebSocket broadcast adds latency to trading | Medium | High | Use separate goroutines, buffered channels |
| Memory leak from abandoned clients | Medium | Medium | Implement connection timeout, health checks |
| Frontend bugs affect engine | Low | High | Decouple completely, use separate ports |
| Authentication complexity | Medium | Low | Start with optional auth, add later |

## Conclusion

The live_server provides critical **monitoring and visualization** capabilities that the market_maker currently lacks. Integration is recommended via a **hybrid approach**:

1. Extract reusable components from live_server
2. Embed WebSocket server into market_maker as optional feature
3. Maintain standalone mode for development
4. Incrementally add grid-specific visualizations

This approach provides:
- ✅ Visual monitoring for operators
- ✅ Real-time alerts and notifications  
- ✅ Professional charting via TradingView
- ✅ No impact on trading engine performance
- ✅ Gradual migration path
