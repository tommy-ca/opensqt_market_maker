import pytest
import asyncio
from unittest.mock import MagicMock, AsyncMock, patch
import ccxt
from opensqt.market_maker.v1 import exchange_pb2, types_pb2
from opensqt.market_maker.v1 import resources_pb2 as models_pb2
from google.type import decimal_pb2
from src.connector.binance import BinanceConnector
import grpc


@pytest.fixture
def connector():
    return BinanceConnector(api_key="test", secret_key="test")


@pytest.mark.asyncio
async def test_idempotent_order_placement(connector):
    # Simulate first request failing with a network timeout but actually reaching the exchange
    # Second request returns DuplicateOrderId
    connector.exchange.create_order = AsyncMock(
        side_effect=[
            ccxt.NetworkError("timeout"),
            ccxt.DuplicateOrderId("duplicate order id"),
        ]
    )

    # Mock fetch_order to return the "existing" order
    mock_order = {
        "id": "123456",
        "clientOrderId": "unique_id_123",
        "symbol": "BTC/USDT",
        "side": "buy",
        "type": "limit",
        "status": "open",
        "price": "50000.0",
        "amount": "1.0",
        "filled": "0.0",
        "average": "0.0",
        "timestamp": 1568879465650,
    }
    connector.exchange.fetch_order = AsyncMock(return_value=mock_order)

    context = AsyncMock()
    request = models_pb2.PlaceOrderRequest(
        symbol="BTC/USDT",
        side=types_pb2.ORDER_SIDE_BUY,
        type=types_pb2.ORDER_TYPE_LIMIT,
        quantity=decimal_pb2.Decimal(value="1.0"),
        price=decimal_pb2.Decimal(value="50000.0"),
        client_order_id="unique_id_123",
    )

    # The call should now succeed because it retries on NetworkError,
    # then on second try gets DuplicateOrderId, then fetches the existing order.
    resp = await connector.PlaceOrder(request, context)

    assert resp.order_id == 123456
    assert resp.client_order_id == "unique_id_123"
    assert resp.status == types_pb2.ORDER_STATUS_NEW

    connector.exchange.create_order.assert_called()
    assert connector.exchange.create_order.call_count == 2
    connector.exchange.fetch_order.assert_called_once()


@pytest.mark.asyncio
async def test_workflow_retry_transient_error(connector):
    # This test verifies that we HAVE a retry policy for transient errors
    # Currently the connector doesn't have internal retries for Unary calls
    connector.exchange.fetch_ticker = AsyncMock(
        side_effect=[ccxt.NetworkError("temporary failure"), {"last": 50000.0}]
    )

    context = AsyncMock()
    request = exchange_pb2.GetLatestPriceRequest(symbol="BTC/USDT")

    # If no retries, it should fail on first call
    # We WANT it to succeed on second internal try
    try:
        resp = await connector.GetLatestPrice(request, context)
        # If we reached here, it retried and succeeded
        assert resp.price.value == "50000.0"
    except ccxt.NetworkError:
        # Failed immediately, means no retries
        pytest.fail("Connector did not retry transient network error")
