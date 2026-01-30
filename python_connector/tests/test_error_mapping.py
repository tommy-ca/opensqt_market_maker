import pytest
import asyncio
from unittest.mock import MagicMock, AsyncMock, patch
import ccxt
from opensqt.market_maker.v1 import exchange_pb2, resources_pb2 as models_pb2, types_pb2
from google.type import decimal_pb2
from src.connector.binance import BinanceConnector
import grpc


@pytest.fixture
def connector():
    return BinanceConnector(api_key="test", secret_key="test")


@pytest.mark.asyncio
async def test_error_mapping_insufficient_funds(connector):
    connector.exchange.create_order = AsyncMock(
        side_effect=ccxt.InsufficientFunds("balance not enough")
    )
    context = AsyncMock()

    with pytest.raises(ccxt.InsufficientFunds):
        await connector.PlaceOrder(
            models_pb2.PlaceOrderRequest(
                symbol="BTC/USDT",
                side=types_pb2.ORDER_SIDE_BUY,
                type=types_pb2.ORDER_TYPE_LIMIT,
                quantity=decimal_pb2.Decimal(value="1.0"),
                price=decimal_pb2.Decimal(value="50000.0"),
            ),
            context,
        )
    context.abort.assert_called_once_with(
        grpc.StatusCode.RESOURCE_EXHAUSTED, "balance not enough"
    )


@pytest.mark.asyncio
async def test_error_mapping_order_not_found(connector):
    connector.exchange.cancel_order = AsyncMock(
        side_effect=ccxt.OrderNotFound("order not found")
    )
    context = AsyncMock()

    with pytest.raises(ccxt.OrderNotFound):
        await connector.CancelOrder(
            exchange_pb2.CancelOrderRequest(symbol="BTC/USDT", order_id=123), context
        )
    context.abort.assert_called_once_with(grpc.StatusCode.NOT_FOUND, "order not found")


@pytest.mark.asyncio
async def test_error_mapping_rate_limit(connector):
    connector.exchange.fetch_balance = AsyncMock(
        side_effect=ccxt.RateLimitExceeded("too many requests")
    )
    context = AsyncMock()

    with pytest.raises(ccxt.RateLimitExceeded):
        await connector.GetAccount(exchange_pb2.GetAccountRequest(), context)

    context.abort.assert_called_once_with(
        grpc.StatusCode.RESOURCE_EXHAUSTED, "too many requests"
    )
