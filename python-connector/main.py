import argparse
import asyncio
import logging
import os
from collections.abc import Awaitable, Callable
from typing import Any

import grpc
from grpc_health.v1 import health, health_pb2, health_pb2_grpc
from opensqt.market_maker.v1 import exchange_pb2_grpc

from src.connector.binance import BinanceConnector

# Setup structured logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("connector")


class AuthInterceptor(grpc.aio.ServerInterceptor):
    def __init__(self, token: str, mandatory: bool = True) -> None:
        self._token = token
        self._mandatory = mandatory

    async def intercept_service(
        self,
        continuation: Callable[
            [grpc.HandlerCallDetails], Awaitable[grpc.RpcMethodHandler]
        ],
        handler_call_details: grpc.HandlerCallDetails,
    ) -> grpc.RpcMethodHandler:
        # Allow health checks without auth
        if handler_call_details.method.endswith(
            "/Check"
        ) or handler_call_details.method.endswith("/Watch"):
            return await continuation(handler_call_details)

        metadatas = dict(handler_call_details.invocation_metadata)
        auth_header = metadatas.get("authorization")

        if not self._token and not self._mandatory:
            return await continuation(handler_call_details)

        if auth_header != f"Bearer {self._token}":
            return self._abort_unauthenticated(handler_call_details)

        return await continuation(handler_call_details)

    def _abort_unauthenticated(
        self, handler_call_details: grpc.HandlerCallDetails
    ) -> grpc.RpcMethodHandler:
        async def abort_handler(
            request: Any, context: grpc.aio.ServicerContext
        ) -> None:
            await context.abort(
                grpc.StatusCode.UNAUTHENTICATED, "Invalid or missing auth token"
            )

        is_request_streaming = getattr(handler_call_details, "request_streaming", False)
        is_response_streaming = getattr(
            handler_call_details, "response_streaming", False
        )

        if is_request_streaming and is_response_streaming:
            return grpc.stream_stream_rpc_method_handler(abort_handler)
        if is_request_streaming:
            return grpc.stream_unary_rpc_method_handler(abort_handler)
        if is_response_streaming:
            return grpc.unary_stream_rpc_method_handler(abort_handler)
        return grpc.unary_unary_rpc_method_handler(abort_handler)


async def serve() -> None:
    parser = argparse.ArgumentParser(description="Binance gRPC Connector (Python)")
    parser.add_argument("--host", type=str, default="127.0.0.1", help="Host to bind to")
    parser.add_argument("--port", type=int, default=50051, help="gRPC server port")
    parser.add_argument(
        "--exchange_type",
        type=str,
        default="futures",
        choices=["spot", "futures"],
        help="Binance exchange type",
    )
    parser.add_argument("--tls_cert", type=str, help="Path to TLS certificate file")
    parser.add_argument("--tls_key", type=str, help="Path to TLS private key file")
    parser.add_argument(
        "--auth_token", type=str, help="Shared secret for authentication"
    )

    args = parser.parse_args()

    api_key = os.environ.get("BINANCE_API_KEY", "")
    secret_key = os.environ.get("BINANCE_SECRET_KEY", "")

    if not api_key or not secret_key:
        logger.warning(
            "BINANCE_API_KEY or BINANCE_SECRET_KEY not set. Private RPCs will fail."
        )

    interceptors = []
    auth_token = args.auth_token or os.environ.get("CONNECTOR_AUTH_TOKEN")
    if auth_token:
        if not (args.tls_cert and args.tls_key) and args.host != "127.0.0.1":
            logger.critical(
                "❌ ERROR: TLS is REQUIRED when using authentication on non-loopback address!"
            )
            return
        logger.info("Authentication enabled with shared token")
        interceptors.append(AuthInterceptor(auth_token, mandatory=True))
    else:
        if args.host != "127.0.0.1":
            logger.critical(
                "❌ ERROR: Auth token is REQUIRED when binding to non-loopback address!"
            )
            return
        logger.warning(
            "⚠️  Running without authentication on loopback. Use only for local development."
        )
        interceptors.append(AuthInterceptor("", mandatory=False))

    server = grpc.aio.server(interceptors=interceptors)
    connector = BinanceConnector(api_key, secret_key, args.exchange_type)
    exchange_pb2_grpc.add_ExchangeServiceServicer_to_server(connector, server)

    # Add Health Service
    health_servicer = health.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)
    health_servicer.set(
        "opensqt.market_maker.v1.ExchangeService",
        health_pb2.HealthCheckResponse.SERVING,
    )
    health_servicer.set("", health_pb2.HealthCheckResponse.SERVING)

    listen_addr = f"{args.host}:{args.port}"

    if args.tls_cert and args.tls_key:
        with open(args.tls_cert, "rb") as f:
            cert = f.read()
        with open(args.tls_key, "rb") as f:
            key = f.read()
        server_credentials = grpc.ssl_server_credentials([(key, cert)])
        server.add_secure_port(listen_addr, server_credentials)
        logger.info("Starting SECURE gRPC server on %s (TLS enabled)", listen_addr)
    else:
        server.add_insecure_port(listen_addr)
        logger.warning("Starting INSECURE gRPC server on %s (No TLS)", listen_addr)
        if args.host != "127.0.0.1":
            logger.warning("⚠️  Server is exposed to the network without encryption!")

    await server.start()

    try:
        await server.wait_for_termination()
    finally:
        await connector.stop()


if __name__ == "__main__":
    asyncio.run(serve())
