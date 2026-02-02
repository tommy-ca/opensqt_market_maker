## 1. Overview
The Bybit Adapter implements the `IExchange` interface for Bybit V5 API (Unified Trading).

## 2. Authentication & Security
- **Type**: API Key + HMAC-SHA256 Signature
- **Headers**:
  - `X-BAPI-API-KEY`: API Key
  - `X-BAPI-SIGN`: HMAC_SHA256(timestamp + key + recv_window + body, secret)
  - `X-BAPI-TIMESTAMP`: Server time in ms
  - `X-BAPI-RECV-WINDOW`: Recv window (default 5000)

## 3. API Mapping (V5 Unified)

### REST Endpoints
| Operation | Internal Method | Bybit Endpoint | Method | Parameters |
| :--- | :--- | :--- | :--- | :--- |
| **Place Order** | `PlaceOrder` | `/v5/order/create` | `POST` | `category`, `symbol`, `side`, `orderType`, `qty`, `price`, `orderLinkId` |
| **Cancel Order** | `CancelOrder` | `/v5/order/cancel` | `POST` | `category`, `symbol`, `orderId` |
| **Batch Place** | `BatchPlaceOrders` | `/v5/order/create-batch` | `POST` | List of orders |
| **Batch Cancel** | `BatchCancelOrders` | `/v5/order/cancel-batch` | `POST` | List of orderIds |
| **Account** | `GetAccount` | `/v5/account/wallet-balance` | `GET` | `accountType=UNIFIED` |
| **Positions** | `GetPositions` | `/v5/position/list` | `GET` | `category=linear`, `symbol` |

### WebSocket Streams
- **Base URL**: `wss://stream.bybit.com/v5/public/linear` (Public), `wss://stream.bybit.com/v5/private` (Private)
- **Authentication**: `auth` op with signature.
- **Channels**:
  - `tickers`: Market price updates (`tickers.{symbol}`).
  - `order`: User order updates.
  - `position`: User position updates.

## 4. Data Models Mapping

### Order Status
| Bybit Status | Internal `OrderStatus` |
| :--- | :--- |
| `New` | `OrderStatusNew` |
| `PartiallyFilled` | `OrderStatusPartiallyFilled` |
| `Filled` | `OrderStatusFilled` |
| `Cancelled` | `OrderStatusCanceled` |

## 5. Implementation Plan
1. **Signer**: Implement V5 signature logic.
2. **REST Client**: Implement Order and Account endpoints.
3. **WebSocket**: Implement Public and Private streams.
