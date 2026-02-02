import pytest
import grpc
from opensqt.market_maker.v1 import exchange_pb2
from opensqt.market_maker.v1 import exchange_pb2_grpc
from concurrent import futures


class MockExchangeService(exchange_pb2_grpc.ExchangeServiceServicer):
    def GetName(self, request, context):
        return exchange_pb2.GetNameResponse(name="mock-python")


@pytest.fixture
def grpc_server():
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
    exchange_pb2_grpc.add_ExchangeServiceServicer_to_server(
        MockExchangeService(), server
    )
    port = server.add_insecure_port("[::]:0")
    server.start()
    yield f"localhost:{port}"
    server.stop(None)


def test_get_name(grpc_server):
    with grpc.insecure_channel(grpc_server) as channel:
        stub = exchange_pb2_grpc.ExchangeServiceStub(channel)
        response = stub.GetName(exchange_pb2.GetNameRequest())
        assert response.name == "mock-python"
