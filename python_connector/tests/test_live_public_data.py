import asyncio
import grpc
from opensqt.market_maker.v1 import exchange_pb2
from opensqt.market_maker.v1 import exchange_pb2_grpc
import sys
import os

# Ensure we can import from the current directory
sys.path.append(os.path.abspath("."))


async def test_public_market_data():
    # 1. Start the connector in the background (or assume it's already running for integration test)
    # For a self-contained script, we'll import and use the servicer directly without a real network if possible,
    # but a real gRPC test over loopback is better.

    from src.connector.binance import BinanceConnector
    from concurrent import futures

    # Start gRPC server on a random port
    server = grpc.aio.server()
    connector = BinanceConnector("", "")  # Empty keys for public data
    exchange_pb2_grpc.add_ExchangeServiceServicer_to_server(connector, server)
    port = server.add_insecure_port("[::]:0")
    await server.start()

    target = f"localhost:{port}"
    print(f"Test server started on {target}")

    try:
        async with grpc.aio.insecure_channel(target) as channel:
            stub = exchange_pb2_grpc.ExchangeServiceStub(channel)

            # Test GetLatestPrice
            symbol = "BTC/USDT"
            print(f"Fetching latest price for {symbol}...")
            response = await stub.GetLatestPrice(
                exchange_pb2.GetLatestPriceRequest(symbol=symbol)
            )
            print(f"Price: {response.price}")
            assert float(response.price) > 0

            # Test SubscribePrice (stream)
            print(
                f"Subscribing to price updates for {symbol} (waiting for 3 updates)..."
            )
            stream = stub.SubscribePrice(
                exchange_pb2.SubscribePriceRequest(symbol=symbol)
            )
            count = 0
            async for update in stream:
                print(
                    f"Update {count + 1}: {update.price} at {update.timestamp.ToDatetime()}"
                )
                count += 1
                if count >= 3:
                    break

    finally:
        await connector.stop()
        await server.stop(None)


if __name__ == "__main__":
    asyncio.run(test_public_market_data())
