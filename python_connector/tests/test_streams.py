import pytest
import asyncio
from unittest.mock import AsyncMock, patch, MagicMock
from src.connector.binance import BinanceConnector
from opensqt.market_maker.v1 import exchange_pb2
from opensqt.market_maker.v1 import resources_pb2 as models_pb2
from google.type import decimal_pb2


@pytest.mark.asyncio
async def test_subscribe_price_multi_symbol():
    with patch("ccxt.pro.binance") as mock_ccxt:
        mock_instance = mock_ccxt.return_value

        # We need watch_tickers to return values for all symbols
        async def side_effect(symbols):
            await asyncio.sleep(0.01)  # Small delay to prevent tight loop
            return {
                "BTC/USDT": {
                    "symbol": "BTC/USDT",
                    "last": 50000.0,
                    "timestamp": 1600000000000,
                },
                "ETH/USDT": {
                    "symbol": "ETH/USDT",
                    "last": 3000.0,
                    "timestamp": 1600000000000,
                },
            }

        mock_instance.watch_tickers = AsyncMock(side_effect=side_effect)
        mock_instance.close = AsyncMock()

        connector = BinanceConnector("key", "secret")
        connector.exchange_pro = mock_instance  # Force pro instance

        request = exchange_pb2.SubscribePriceRequest(symbols=["BTC/USDT", "ETH/USDT"])

        # Consume stream
        stream = connector.SubscribePrice(request, None)

        results = []
        # Get 2 updates (one for each symbol)
        results.append(await anext(stream))
        results.append(await anext(stream))

        # Verify we got both symbols
        symbols_received = {r.symbol for r in results}
        assert "BTC/USDT" in symbols_received
        assert "ETH/USDT" in symbols_received

        await stream.aclose()
        await connector.stop()


@pytest.mark.asyncio
async def test_subscribe_account():
    with patch("ccxt.pro.binance") as mock_ccxt:
        mock_instance = mock_ccxt.return_value

        mock_instance.watch_balance = AsyncMock(
            return_value={"total": {"USDT": 1000.0}, "free": {"USDT": 500.0}}
        )
        mock_instance.close = AsyncMock()

        connector = BinanceConnector("key", "secret")
        connector.exchange_pro = mock_instance

        request = exchange_pb2.SubscribeAccountRequest()
        stream = connector.SubscribeAccount(request, None)

        # Get one update
        account = await anext(stream)
        assert account.total_wallet_balance.value == "1000.0"
        assert account.available_balance.value == "500.0"

        await stream.aclose()
        await connector.stop()


@pytest.mark.asyncio
async def test_subscribe_positions():
    with patch("ccxt.pro.binance") as mock_ccxt:
        mock_instance = mock_ccxt.return_value

        mock_instance.watch_positions = AsyncMock(
            return_value=[
                {
                    "symbol": "BTC/USDT",
                    "contracts": 0.5,
                    "entryPrice": 49000.0,
                    "markPrice": 50000.0,
                    "unrealizedPnl": 500.0,
                    "leverage": 10,
                    "marginType": "cross",
                    "isolatedWallet": 0,
                }
            ]
        )
        mock_instance.close = AsyncMock()

        connector = BinanceConnector("key", "secret")
        connector.exchange_pro = mock_instance

        request = exchange_pb2.SubscribePositionsRequest(symbol="BTC/USDT")
        stream = connector.SubscribePositions(request, None)

        position = await anext(stream)
        assert position.symbol == "BTC/USDT"
        assert position.size.value == "0.5"
        assert position.unrealized_pnl.value == "500.0"

        await stream.aclose()
        await connector.stop()
