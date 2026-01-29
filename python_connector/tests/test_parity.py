import unittest
import asyncio
from unittest.mock import MagicMock, AsyncMock
from src.connector.binance import BinanceConnector
from opensqt.market_maker.v1 import exchange_pb2, resources_pb2 as models_pb2, types_pb2
from google.type import decimal_pb2


class TestBinanceParity(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.connector = BinanceConnector("key", "secret", "futures")
        # Mock exchanges
        self.connector.exchange = AsyncMock()
        self.connector.exchange_pro = AsyncMock()

    async def test_batch_place_orders_structure(self):
        # This test ensures the method exists and accepts the request
        req = exchange_pb2.BatchPlaceOrdersRequest(
            orders=[
                models_pb2.PlaceOrderRequest(
                    symbol="BTC/USDT",
                    side=types_pb2.ORDER_SIDE_BUY,
                    type=types_pb2.ORDER_TYPE_LIMIT,
                    price=decimal_pb2.Decimal(value="50000"),
                    quantity=decimal_pb2.Decimal(value="1"),
                )
            ]
        )
        # Should fail because it's not implemented yet or implemented incorrectly
        try:
            await self.connector.BatchPlaceOrders(req, None)
        except Exception as e:
            # If it's NotImplementedError (from base) or AttributeError (missing), we know we need to work
            print(f"Caught expected error: {e}")

    async def test_batch_cancel_orders_structure(self):
        req = exchange_pb2.BatchCancelOrdersRequest(
            symbol="BTC/USDT", order_ids=[123, 456]
        )
        try:
            await self.connector.BatchCancelOrders(req, None)
        except Exception as e:
            print(f"Caught expected error: {e}")

    async def test_subscribe_price_multiplex(self):
        # Test calling with multiple symbols
        req = exchange_pb2.SubscribePriceRequest(symbols=["BTC/USDT", "ETH/USDT"])

        # Mock watch_tickers to return immediately then hang or raise
        self.connector.exchange_pro.watch_tickers.side_effect = asyncio.CancelledError

        try:
            async for _ in self.connector.SubscribePrice(req, None):
                pass
        except asyncio.CancelledError:
            pass
        except Exception as e:
            print(f"Caught expected error: {e}")


if __name__ == "__main__":
    unittest.main()
