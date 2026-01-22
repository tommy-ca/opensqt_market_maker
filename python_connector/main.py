import asyncio
import grpc
import os
import argparse
from opensqt.market_maker.v1 import exchange_pb2_grpc
from src.connector.binance import BinanceConnector
from concurrent import futures
from grpc_health.v1 import health
from grpc_health.v1 import health_pb2_grpc


async def serve():
    parser = argparse.ArgumentParser(description="Binance gRPC Connector (Python)")
    parser.add_argument("--port", type=int, default=50051, help="gRPC server port")
    parser.add_argument(
        "--exchange_type",
        type=str,
        default="futures",
        choices=["spot", "futures"],
        help="Binance exchange type",
    )
    args = parser.parse_args()

    api_key = os.environ.get("BINANCE_API_KEY", "")
    secret_key = os.environ.get("BINANCE_SECRET_KEY", "")

    if not api_key or not secret_key:
        print("Warning: BINANCE_API_KEY or BINANCE_SECRET_KEY not set")

    server = grpc.aio.server()
    connector = BinanceConnector(api_key, secret_key, args.exchange_type)
    exchange_pb2_grpc.add_ExchangeServiceServicer_to_server(connector, server)

    # Add Health Service
    health_servicer = health.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)
    health_servicer.set(
        "opensqt.market_maker.v1.ExchangeService",
        health.health_pb2.HealthCheckResponse.SERVING,
    )
    health_servicer.set("", health.health_pb2.HealthCheckResponse.SERVING)

    listen_addr = f"[::]:{args.port}"
    server.add_insecure_port(listen_addr)

    print(f"Starting Binance {args.exchange_type} connector on {listen_addr}")
    await server.start()

    try:
        await server.wait_for_termination()
    finally:
        await connector.stop()


if __name__ == "__main__":
    asyncio.run(serve())
