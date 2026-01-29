import pytest
import asyncio
from unittest.mock import AsyncMock, patch, MagicMock
from src.connector.binance import BinanceConnector
from opensqt.market_maker.v1 import exchange_pb2
from opensqt.market_maker.v1 import resources_pb2 as models_pb2, types_pb2
from google.protobuf.timestamp_pb2 import Timestamp


@pytest.mark.asyncio
async def test_binance_get_name():
    connector = BinanceConnector("key", "secret")
    # Wrap in async task if needed, but here it's simple
    response = await connector.GetName(None, None)
    assert response.name == "binance"
    await connector.stop()


@pytest.mark.asyncio
async def test_binance_get_latest_price():
    with patch("ccxt.async_support.binance") as mock_ccxt:
        mock_instance = mock_ccxt.return_value
        mock_instance.fetch_ticker = AsyncMock(return_value={"last": 50000.5})
        mock_instance.close = AsyncMock()

        connector = BinanceConnector("key", "secret")
        # We need to manually set the exchange because of the patch
        connector.exchange = mock_instance

        request = exchange_pb2.GetLatestPriceRequest(symbol="BTC/USDT")
        response = await connector.GetLatestPrice(request, None)

        assert response.price.value == "50000.5"
        mock_instance.fetch_ticker.assert_called_with("BTC/USDT")
        await connector.stop()


@pytest.mark.asyncio
async def test_binance_place_order():
    with patch("ccxt.async_support.binance") as mock_ccxt:
        mock_instance = mock_ccxt.return_value
        mock_instance.create_order = AsyncMock(
            return_value={
                "id": "12345",
                "symbol": "BTC/USDT",
                "side": "buy",
                "type": "limit",
                "amount": 1.0,
                "price": 45000.0,
                "filled": 0.0,
                "average": 0.0,
                "status": "open",
                "timestamp": 1600000000000,
                "clientOrderId": "my_id",
            }
        )
        mock_instance.close = AsyncMock()

        connector = BinanceConnector("key", "secret")
        connector.exchange = mock_instance

        request = models_pb2.PlaceOrderRequest(
            symbol="BTC/USDT",
            side=types_pb2.ORDER_SIDE_BUY,
            type=types_pb2.ORDER_TYPE_LIMIT,
            quantity=models_pb2.google_dot_type_dot_decimal__pb2.Decimal(value="1.0"),
            price=models_pb2.google_dot_type_dot_decimal__pb2.Decimal(value="45000.0"),
            client_order_id="my_id",
        )

        response = await connector.PlaceOrder(request, None)

        assert response.order_id == 12345
        assert response.client_order_id == "my_id"
        assert response.status == types_pb2.ORDER_STATUS_NEW
        await connector.stop()
