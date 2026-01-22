# Phase 18.4: Python Connector Parity - Technical Specification

**Project**: OpenSQT Market Maker
**Status**: FINAL

---

## 1. Overview

This phase ensures the Python gRPC Connector (`exchange_connector`) reaches full feature parity with the Go connector (`RemoteExchange`) and the latest protocol definitions. This is critical to support the new multi-symbol architecture and advanced order types while maintaining the flexibility of Python exchange adapters (CCXT).

## 2. Gap Analysis

| Feature | Go Connector Support | Current Python Connector | Required Action |
| :--- | :--- | :--- | :--- |
| **Batch Order Placement** | ✅ Implemented (`BatchPlaceOrders`) | ✅ Implemented | Implemented using `ccxt.create_orders` (if supported) with fallback to concurrent `create_order`. |
| **Batch Order Cancellation** | ✅ Implemented (`BatchCancelOrders`) | ✅ Implemented | Implemented using `ccxt.cancel_orders` or concurrent calls. |
| **Multi-Symbol Price Stream** | ✅ Implemented (`SubscribePrice` takes `[]string`) | ✅ Implemented | Multiplex streams using `asyncio.Queue` pattern. |
| **Account Streaming** | ✅ Implemented (`SubscribeAccount`) | ✅ Implemented | Using `watch_balance`. |
| **Position Streaming** | ✅ Implemented (`SubscribePositions`) | ✅ Implemented | Using `watch_positions`. |
| **Klines Multiplexing** | ✅ Implemented | ✅ Implemented | Multiplex streams using `asyncio.Queue` pattern. |

## 3. Implementation Details

### 3.1 Batch Operations

**`BatchPlaceOrders`**:
- Attempts to use `exchange.create_orders` if available (e.g., Binance).
- Fallback: Uses `asyncio.gather` to execute individual `PlaceOrder` calls concurrently.
- Maps results back to `BatchPlaceOrdersResponse`.

**`BatchCancelOrders`**:
- Attempts to use `exchange.cancel_orders`.
- Fallback: Concurrent `cancel_order` calls.

### 3.2 Streaming Updates

**`SubscribePrice`** and **`SubscribeKlines`**:
- Input: `repeated string symbols`.
- Logic:
    - Creates a background task (producer) for each symbol loop.
    - Producers feed a shared `asyncio.Queue`.
    - Main loop consumes from the queue and yields to the gRPC stream.
    - Handles `asyncio.CancelledError` to clean up producers.

**`SubscribeAccount` / `SubscribePositions`**:
- Utilizes `watch_balance` and `watch_positions` from `ccxt.pro`.
- Maps CCXT structures to `models_pb2` messages.

### 3.3 Proto Regeneration

- **Synchronization**: Copied `market_maker/api/proto/opensqt/market_maker/v1/exchange.proto` and `models.proto` to `python_connector/proto/`.
- **Generation**: Ran `buf generate` (via `make proto`) which updated `python_connector/opensqt/market_maker/v1/`.

## 4. Testing

- **Unit Tests**: `python_connector/tests/test_batch_ops.py` and `python_connector/tests/test_streams.py` verify new functionality using `unittest.mock` and `pytest-asyncio`.
- **Existing Tests**: Verified `test_binance_connector.py` still passes.

## 5. Deployment

- Update `Dockerfile` if new dependencies are needed.
- Ensure `docker-compose.yml` mounts the correct volumes or rebuilds the image.
