## 1. Overview
The Binance Adapter implements the `IExchange` interface for both Binance Perpetual Futures and Binance Spot APIs. It handles authentication, order management, and real-time data streaming.

## 2. Authentication & Security
- **Type**: API Key + HMAC-SHA256 Signature
- **Headers**:
  - `X-MBX-APIKEY`: User's API Key
- **Signature Param**: `&signature=HEX(HMAC_SHA256(secret, query_string))`
- **Timestamp**:
  - All requests must include `timestamp` (server time in ms).
  - Adapter must sync time (`GET /fapi/v1/time` or `/api/v3/time`) on startup and periodic drift checks.
  - `recvWindow`: Set to 5000ms by default to handle network jitter.

## 3. API Mapping (Futures)

### REST Endpoints
| Operation | Internal Method | Binance Endpoint | Method | Parameters |
| :--- | :--- | :--- | :--- | :--- |
| **Place Order** | `PlaceOrder` | `/fapi/v1/order` | `POST` | `symbol`, `side`, `type`, `quantity`, `price`, `timeInForce`, `newClientOrderId` |
| **Cancel Order** | `CancelOrder` | `/fapi/v1/order` | `DELETE` | `symbol`, `orderId` OR `origClientOrderId` |
| **Get Order** | `GetOrder` | `/fapi/v1/order` | `GET` | `symbol`, `orderId` OR `origClientOrderId` |
| **Open Orders** | `GetOpenOrders` | `/fapi/v1/openOrders` | `GET` | `symbol` |
| **Account** | `GetAccount` | `/fapi/v2/account` | `GET` | None |
| **Positions** | `GetPositions` | `/fapi/v2/positionRisk` | `GET` | `symbol` (optional) |
| **Exchange Info** | `Init` | `/fapi/v1/exchangeInfo` | `GET` | None (Used for precision/limits) |

### WebSocket Streams
- **Base URL**: `wss://fstream.binance.com/ws`
- **Price Stream**: `<symbol>@bookTicker` (Fastest BBO updates)
- **Kline Stream**: `<symbol>@kline_<interval>`
- **User Stream**:
  1. `POST /fapi/v1/listenKey` to get key.
  2. Connect to `wss://fstream.binance.com/ws/<listenKey>`.
  3. Keep-alive via `PUT /fapi/v1/listenKey` every 30m.

## 4. Data Models Mapping

### Order Status
| Binance Status | Internal `OrderStatus` |
| :--- | :--- |
| `NEW` | `OrderStatusNew` |
| `PARTIALLY_FILLED` | `OrderStatusPartiallyFilled` |
| `FILLED` | `OrderStatusFilled` |
| `CANCELED` | `OrderStatusCanceled` |
| `REJECTED` | `OrderStatusRejected` |
| `EXPIRED` | `OrderStatusExpired` |

### Order Types
| Binance Type | Internal `OrderType` |
| :--- | :--- |
| `LIMIT` | `OrderTypeLimit` |
| `MARKET` | `OrderTypeMarket` |
| `STOP` | *Not currently supported* |

## 5. Resilience Strategy
- **HTTP 429 (Rate Limit)**:
  - Respect `Retry-After` header.
  - Use `failsafe-go` exponential backoff (initial 1s, max 30s).
- **HTTP 418 (IP Ban)**:
  - **CRITICAL**: Immediate `Panic/Shutdown` to prevent extended bans.
- **WebSocket Disconnect**:
  - Auto-reconnect with Fibonacci backoff (1s, 1s, 2s, 3s, 5s...).
  - Resubscribe to channels after reconnection.

## 6. Implementation Plan (TDD)
1. **Security**: Test signature generation.
2. **Request Building**: Test parameter serialization (bools, floats).
3. **Response Parsing**: Test JSON unmarshalling to internal structs.
4. **Error Handling**: Test mapping of specific error codes (-2010, -1003).
