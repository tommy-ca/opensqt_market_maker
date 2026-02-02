## 1. Overview
The OKX Adapter implements the `IExchange` interface for OKX V5 API.

## 2. Authentication & Security
- **Type**: API Key + HMAC-SHA256 Signature + Passphrase
- **Headers**:
  - `OK-ACCESS-KEY`: API Key
  - `OK-ACCESS-SIGN`: Base64(HMAC_SHA256(timestamp + method + requestPath + body, secret))
  - `OK-ACCESS-TIMESTAMP`: ISO 8601 format (e.g. 2020-12-08T09:08:57.715Z)
  - `OK-ACCESS-PASSPHRASE`: API Passphrase

## 3. API Mapping (V5)

### REST Endpoints
| Operation | Internal Method | OKX Endpoint | Method | Parameters |
| :--- | :--- | :--- | :--- | :--- |
| **Place Order** | `PlaceOrder` | `/api/v5/trade/order` | `POST` | `instId`, `tdMode`, `side`, `ordType`, `sz`, `px` |
| **Cancel Order** | `CancelOrder` | `/api/v5/trade/cancel-order` | `POST` | `instId`, `ordId` |
| **Batch Place** | `BatchPlaceOrders` | `/api/v5/trade/batch-orders` | `POST` | List of orders |
| **Batch Cancel** | `BatchCancelOrders` | `/api/v5/trade/cancel-batch-orders` | `POST` | List of ordIds |
| **Account** | `GetAccount` | `/api/v5/account/balance` | `GET` | `ccy` |
| **Positions** | `GetPositions` | `/api/v5/account/positions` | `GET` | `instType=SWAP` |

### WebSocket Streams
- **Base URL**: `wss://ws.okx.com:8443/ws/v5/public` (Market Data), `wss://ws.okx.com:8443/ws/v5/private` (Private Data)
- **Authentication**: `login` op with signature.
- **Channels**:
  - `tickers`: Market price updates (`instId`).
  - `orders`: User order updates (`instType`).
  - `positions`: User position updates.

## 4. Data Models Mapping

### Order Status
| OKX Status | Internal `OrderStatus` |
| :--- | :--- |
| `live` | `OrderStatusNew` / `OrderStatusPartiallyFilled` |
| `filled` | `OrderStatusFilled` |
| `canceled` | `OrderStatusCanceled` |

## 5. Implementation Plan
1. **Signer**: Implement V5 signature logic.
2. **REST Client**: Implement Order and Account endpoints.
3. **WebSocket**: Implement Public (Price) and Private (Order) streams.
