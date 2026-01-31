import pytest
from unittest.mock import AsyncMock

from opensqt.market_maker.v1 import types_pb2
from opensqt.market_maker.v1 import resources_pb2 as models_pb2
from google.type import decimal_pb2

from src.connector.binance import BinanceConnector


class RecordingExchange:
    def __init__(self):
        self.last_params = None

    async def create_order(self, symbol, order_type, side, amount, price, params):
        self.last_params = params
        return {
            "id": "1",
            "clientOrderId": params.get("clientOrderId", ""),
            "symbol": symbol,
            "side": side,
            "type": order_type,
            "status": "open",
            "price": price or "0",
            "amount": str(amount),
            "filled": "0",
            "average": "0",
            "timestamp": 0,
        }


@pytest.mark.asyncio
async def test_binance_connector_propagates_use_margin_flag():
    # Bypass __init__ to avoid real ccxt construction
    connector = BinanceConnector.__new__(BinanceConnector)
    connector.exchange_type = "spot"
    connector.exchange = RecordingExchange()
    connector.exchange_pro = None

    request = models_pb2.PlaceOrderRequest(
        symbol="BTC/USDT",
        side=types_pb2.ORDER_SIDE_SELL,
        type=types_pb2.ORDER_TYPE_MARKET,
        quantity=decimal_pb2.Decimal(value="1"),
        client_order_id="cid-margin",
        use_margin=True,
    )

    context = AsyncMock()

    await connector.PlaceOrder(request, context)

    assert connector.exchange.last_params is not None
    assert connector.exchange.last_params.get("margin") is True
    assert connector.exchange.last_params.get("clientOrderId") == "cid-margin"
