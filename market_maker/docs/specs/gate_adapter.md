## 1. Overview
The Gate.io Adapter implements the `IExchange` interface for Gate.io Perpetual Futures API (V4).

## 2. Authentication & Security
- **Type**: API Key + HMAC-SHA512 Signature
- **Headers**:
  - `KEY`: User's API Key
  - `SIGN`: Hex(HMAC_SHA512(secret, message))
  - `Timestamp`: Server time in seconds
- **Signature Message**: `method + "\n" + url_path + "\n" + query_string + "\n" + hex(sha512(body)) + "\n" + timestamp`

## 3. API Mapping (Futures)

### REST Endpoints
| Operation | Internal Method | Gate Endpoint | Method | Parameters |
| :--- | :--- | :--- | :--- | :--- |
| **Place Order** | `PlaceOrder` | `/api/v4/futures/usdt/orders` | `POST` | `contract`, `size`, `iceberg`, `price`, `tif`, `text` (ClientOid) |
| **Cancel Order** | `CancelOrder` | `/api/v4/futures/usdt/orders/{id}` | `DELETE` | `order_id` |
| **Get Order** | `GetOrder` | `/api/v4/futures/usdt/orders/{id}` | `GET` | `order_id` |
| **Open Orders** | `GetOpenOrders` | `/api/v4/futures/usdt/orders` | `GET` | `status=open`, `contract` |
| **Account** | `GetAccount` | `/api/v4/futures/usdt/accounts` | `GET` | None |
| **Positions** | `GetPositions` | `/api/v4/futures/usdt/positions` | `GET` | `contract` (optional) |

### WebSocket Streams
- **Base URL**: `wss://fx-ws.gateio.ws/v4/ws/usdt`
- **Authentication**: `auth` channel message.
- **Channels**:
  - `futures.tickers`: Price updates.
  - `futures.candlesticks`: K-Line updates.
  - `futures.orders`: User order updates.
  - `futures.usertrades`: User trade updates.

## 4. Data Models Mapping

### Order Status
| Gate Status | Internal `OrderStatus` |
| :--- | :--- |
| `open` | `OrderStatusNew` |
| `finished` | `OrderStatusFilled` (Check `finish_as` for details) |

## 5. Implementation Plan
1. **Signer**: Implement SHA512 signing logic.
2. **REST Client**: Implement V4 Futures endpoints.
3. **WebSocket**: Implement V4 WebSocket protocol with Ping/Pong.
