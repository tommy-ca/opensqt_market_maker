import pytest
from unittest.mock import AsyncMock
import ccxt
import grpc
from src.connector.errors import handle_ccxt_exception


class MockServicer:
    @handle_ccxt_exception
    async def some_method(self, request, context):
        raise ccxt.InsufficientFunds("insufficient funds")

    @handle_ccxt_exception
    async def method_with_extra_args(self, request, context, extra):
        raise ccxt.InsufficientFunds("insufficient funds")


@pytest.mark.asyncio
async def test_decorator_finds_context_as_keyword():
    servicer = MockServicer()
    context = AsyncMock(spec=grpc.ServicerContext)
    context.abort = AsyncMock()

    # This should work if we pass context as keyword
    with pytest.raises(ccxt.InsufficientFunds):
        await servicer.some_method("request", context=context)

    context.abort.assert_called_once()


@pytest.mark.asyncio
async def test_decorator_finds_context_not_at_end():
    servicer = MockServicer()
    context = AsyncMock(spec=grpc.ServicerContext)
    context.abort = AsyncMock()

    with pytest.raises(ccxt.InsufficientFunds):
        await servicer.method_with_extra_args("request", context, "extra")

    context.abort.assert_called_once()
