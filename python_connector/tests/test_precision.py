import pytest
import asyncio
from unittest.mock import AsyncMock, patch
from src.connector.binance import BinanceConnector
from opensqt.market_maker.v1 import exchange_pb2, resources_pb2 as models_pb2, types_pb2
from google.type import decimal_pb2
from decimal import Decimal


@pytest.mark.asyncio
async def test_binance_precision_place_order():
    with patch("ccxt.async_support.binance") as mock_ccxt:
        mock_instance = mock_ccxt.return_value
        mock_instance.create_order = AsyncMock(
            return_value={
                "id": "12345",
                "symbol": "BTC/USDT",
                "side": "buy",
                "type": "limit",
                "amount": 0.00000001,
                "price": 50000.00000001,
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

        # Test with high precision strings
        quantity_str = "0.00000001"
        price_str = "50000.00000001"

        request = models_pb2.PlaceOrderRequest(
            symbol="BTC/USDT",
            side=types_pb2.ORDER_SIDE_BUY,
            type=types_pb2.ORDER_TYPE_LIMIT,
            quantity=decimal_pb2.Decimal(value=quantity_str),
            price=decimal_pb2.Decimal(value=price_str),
            client_order_id="my_id",
        )

        response = await connector.PlaceOrder(request, None)

        # Verify that create_order was called with strings (or at least the exact values)
        # Note: In our implementation we pass request.quantity.value directly which is a string.
        args, kwargs = mock_instance.create_order.call_args
        # args are (symbol, order_type, side, amount, price, params)
        assert args[3] == quantity_str
        assert args[4] == price_str

        # Verify response retains precision (converted back from CCXT return which might be float,
        # but our _map_order uses str() which helps if CCXT hasn't lost it already)
        # In a real scenario, CCXT might return floats, but if it returns strings we are even better.
        # Here we mocked it with floats in return_value, so str() might show issues if not careful,
        # but 0.00000001 is exactly representable in float if it's a power of 2, but it's not.
        # However, str(0.00000001) usually works fine in Python for these small numbers.

        assert (
            response.quantity.value == "1e-08"
            or response.quantity.value == "0.00000001"
        )
        # Actually str(1e-08) is '1e-08'

        await connector.stop()


@pytest.mark.asyncio
async def test_get_tickers_precision():
    with patch("ccxt.async_support.binance") as mock_ccxt:
        mock_instance = mock_ccxt.return_value
        # 0.123% -> should be 0.00123
        mock_instance.fetch_tickers = AsyncMock(
            return_value={
                "BTC/USDT": {
                    "symbol": "BTC/USDT",
                    "percentage": 0.123,
                    "last": 50000.0,
                    "timestamp": 1600000000000,
                }
            }
        )
        mock_instance.close = AsyncMock()

        connector = BinanceConnector("key", "secret")
        connector.exchange = mock_instance

        response = await connector.GetTickers(None, None)
        ticker = response.tickers[0]

        # 0.123 / 100 = 0.00123
        assert ticker.price_change_percent.value == "0.00123"

        await connector.stop()


@pytest.mark.asyncio
async def test_get_positions_filtering():
    with patch("ccxt.async_support.binance") as mock_ccxt:
        mock_instance = mock_ccxt.return_value
        mock_instance.fetch_positions = AsyncMock(
            return_value=[
                {
                    "symbol": "BTC/USDT",
                    "contracts": 0.0,
                    "size": 0.0,
                },  # Should be filtered
                {
                    "symbol": "ETH/USDT",
                    "contracts": 0.00000001,
                    "size": 0.00000001,
                },  # Should NOT be filtered
            ]
        )
        mock_instance.close = AsyncMock()

        connector = BinanceConnector("key", "secret")
        connector.exchange = mock_instance

        response = await connector.GetPositions(
            exchange_pb2.GetPositionsRequest(), None
        )

        assert len(response.positions) == 1
        assert response.positions[0].symbol == "ETH/USDT"
        assert (
            response.positions[0].size.value == "1e-08"
            or response.positions[0].size.value == "0.00000001"
        )

        await connector.stop()
