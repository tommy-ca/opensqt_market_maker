import pytest
import asyncio
from unittest.mock import AsyncMock, patch
from src.connector.binance import BinanceConnector
from opensqt.market_maker.v1 import exchange_pb2
from opensqt.market_maker.v1 import models_pb2
from google.type import decimal_pb2


@pytest.mark.asyncio
async def test_batch_place_orders():
    with patch("ccxt.async_support.binance") as mock_ccxt:
        mock_instance = mock_ccxt.return_value

        # Setup mock return values for create_orders (if supported) or create_order
        # Assuming we might try create_orders first (which CCXT supports for some exchanges)
        # But our implementation plan said "use create_orders if available or parallel create_order"
        # Let's assume we implement the parallel version first or check capability.
        # For simplicity, we can mock create_order and verify it's called multiple times if batching isn't natively supported,
        # OR mock create_orders if we implement that.

        # Let's assume the implementation attempts native batching first.
        mock_instance.has = {"createOrders": True}
        mock_instance.create_orders = AsyncMock(
            return_value=[
                {
                    "id": "1",
                    "symbol": "BTC/USDT",
                    "side": "buy",
                    "type": "limit",
                    "amount": 1.0,
                    "price": 50000.0,
                    "status": "open",
                    "clientOrderId": "c1",
                },
                {
                    "id": "2",
                    "symbol": "BTC/USDT",
                    "side": "sell",
                    "type": "limit",
                    "amount": 0.5,
                    "price": 51000.0,
                    "status": "open",
                    "clientOrderId": "c2",
                },
            ]
        )
        mock_instance.close = AsyncMock()

        connector = BinanceConnector("key", "secret")
        connector.exchange = mock_instance

        req1 = models_pb2.PlaceOrderRequest(
            symbol="BTC/USDT",
            side=models_pb2.ORDER_SIDE_BUY,
            type=models_pb2.ORDER_TYPE_LIMIT,
            quantity=decimal_pb2.Decimal(value="1.0"),
            price=decimal_pb2.Decimal(value="50000.0"),
            client_order_id="c1",
        )
        req2 = models_pb2.PlaceOrderRequest(
            symbol="BTC/USDT",
            side=models_pb2.ORDER_SIDE_SELL,
            type=models_pb2.ORDER_TYPE_LIMIT,
            quantity=decimal_pb2.Decimal(value="0.5"),
            price=decimal_pb2.Decimal(value="51000.0"),
            client_order_id="c2",
        )

        request = exchange_pb2.BatchPlaceOrdersRequest(orders=[req1, req2])

        response = await connector.BatchPlaceOrders(request, None)

        assert len(response.orders) == 2
        assert response.orders[0].client_order_id == "c1"
        assert response.orders[1].client_order_id == "c2"
        assert response.all_success == True

        mock_instance.create_orders.assert_called_once()
        await connector.stop()


@pytest.mark.asyncio
async def test_batch_cancel_orders():
    with patch("ccxt.async_support.binance") as mock_ccxt:
        mock_instance = mock_ccxt.return_value
        mock_instance.has = {"cancelOrders": True}
        mock_instance.cancel_orders = AsyncMock(
            return_value=[{}, {}]
        )  # Return doesn't matter much for cancel
        mock_instance.close = AsyncMock()

        connector = BinanceConnector("key", "secret")
        connector.exchange = mock_instance

        request = exchange_pb2.BatchCancelOrdersRequest(
            symbol="BTC/USDT", order_ids=[1, 2]
        )

        response = await connector.BatchCancelOrders(request, None)

        assert isinstance(response, exchange_pb2.BatchCancelOrdersResponse)
        mock_instance.cancel_orders.assert_called_once()
        # Verify arguments passed to cancel_orders
        # CCXT expects list of ids and symbol
        # await exchange.cancel_orders(ids, symbol)
        call_args = mock_instance.cancel_orders.call_args
        assert "1" in call_args[0][0] or 1 in call_args[0][0]
        assert "BTC/USDT" == call_args[0][1]

        await connector.stop()
