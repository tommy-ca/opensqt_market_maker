import pytest
import ccxt
from unittest.mock import AsyncMock
from src.connector.binance import BinanceConnector
from opensqt.market_maker.v1 import resources_pb2 as models_pb2, types_pb2


@pytest.mark.asyncio
async def test_invalid_enum_validation():
    connector = BinanceConnector("key", "secret")

    # Test invalid side
    request = models_pb2.PlaceOrderRequest(
        symbol="BTC/USDT",
        side=types_pb2.ORDER_SIDE_UNSPECIFIED,
        type=types_pb2.ORDER_TYPE_LIMIT,
    )

    context = AsyncMock()
    with pytest.raises(ccxt.BadRequest):
        await connector.PlaceOrder(request, context)

    # Test invalid type
    request = models_pb2.PlaceOrderRequest(
        symbol="BTC/USDT",
        side=types_pb2.ORDER_SIDE_BUY,
        type=types_pb2.ORDER_TYPE_UNSPECIFIED,
    )

    with pytest.raises(ccxt.BadRequest):
        await connector.PlaceOrder(request, context)

    await connector.stop()
