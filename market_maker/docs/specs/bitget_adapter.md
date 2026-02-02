## 1. Overview
The Bitget Adapter implements the `IExchange` interface for Bitget Futures API. It handles authentication, order management, and real-time data streaming.

## 2. Authentication & Security
- **Type**: API Key + HMAC-SHA256 Signature + Passphrase
- **Headers**:
  - `ACCESS-KEY`: User's API Key
  - `ACCESS-SIGN`: Base64(HMAC_SHA256(secret, timestamp + method + requestPath + body))
  - `ACCESS-TIMESTAMP`: Server time in ms
  - `ACCESS-PASSPHRASE`: User's Passphrase
- **Timestamp**:
  - Adapter must sync time on startup.
  - Recommended `recvWindow` handling if drift occurs.

## 3. API Mapping (Futures)

### REST Endpoints
| Operation | Internal Method | Bitget Endpoint | Method | Parameters |
| :--- | :--- | :--- | :--- | :--- |
| **Place Order** | `PlaceOrder` | `/api/v2/mix/order/place-order` | `POST` | `symbol`, `productType`, `marginMode`, `marginCoin`, `size`, `price`, `side`, `tradeSide`, `orderType`, `force`, `clientOid` |
| **Cancel Order** | `CancelOrder` | `/api/v2/mix/order/cancel-order` | `POST` | `symbol`, `productType`, `marginCoin`, `orderId` OR `clientOid` |
| **Batch Place** | `BatchPlaceOrders` | `/api/v2/mix/order/batch-place-order` | `POST` | List of orders |
| **Batch Cancel** | `BatchCancelOrders` | `/api/v2/mix/order/batch-cancel-orders` | `POST` | List of orderIds |
| **Account** | `GetAccount` | `/api/v2/mix/account/account` | `GET` | `productType`, `marginCoin` |
| **Positions** | `GetPositions` | `/api/v2/mix/position/all-position` | `GET` | `productType`, `marginCoin` |

### WebSocket Streams
- **Base URL**: `wss://ws.bitget.com/mix/v1/stream`
- **Authentication**: `login` message with signature.
- **Channels**:
  - `ticker`: Market price updates.
  - `candle`: K-Line updates.
  - `orders`: Private order updates.
  - `positions`: Private position updates.

## 4. Data Models Mapping

### Order Status
| Bitget Status | Internal `OrderStatus` |
| :--- | :--- |
| `new` | `OrderStatusNew` |
| `partially_filled` | `OrderStatusPartiallyFilled` |
| `filled` | `OrderStatusFilled` |
| `cancelled` | `OrderStatusCanceled` |

## 5. Implementation Plan
1. **Signer**: Implement `Signer` struct for Bitget-specific auth.
2. **REST Client**: Implement wrappers for V2 Mix API.
3. **WebSocket**: Implement connection management, login, and subscription.
4. **Resilience**: Handle Bitget specific rate limits and error codes (e.g. `00000` is success).
