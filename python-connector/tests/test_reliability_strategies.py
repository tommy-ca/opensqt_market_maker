import pytest
import grpc
import asyncio
from src.connector.errors import _get_grpc_context, handle_ccxt_exception


class MockContext:
    def __init__(self):
        self.aborted = False
        self.code = None
        self.details = None

    async def abort(self, code, details):
        self.aborted = True
        self.code = code
        self.details = details
        raise grpc.RpcError("Aborted")


@pytest.mark.asyncio
async def test_get_grpc_context_positional():
    context = MockContext()

    async def my_rpc(request, ctx):
        return _get_grpc_context(my_rpc, (None, ctx), {})

    found = await my_rpc(None, context)
    assert found == context


@pytest.mark.asyncio
async def test_get_grpc_context_keyword():
    context = MockContext()

    async def my_rpc(request, context=None):
        return _get_grpc_context(my_rpc, (None,), {"context": context})

    found = await my_rpc(None, context=context)
    assert found == context


@pytest.mark.asyncio
async def test_get_grpc_context_robust_find():
    context = MockContext()

    async def my_rpc(arg1, arg2, arg3):
        return _get_grpc_context(my_rpc, (arg1, arg2, arg3), {})

    # Test finding by .abort method attribute
    found = await my_rpc(1, "something", context)
    assert found == context


@pytest.mark.asyncio
async def test_handle_ccxt_exception_aborts():
    context = MockContext()
    import ccxt

    @handle_ccxt_exception
    async def failing_rpc(request, context):
        raise ccxt.InsufficientFunds("Not enough money")

    with pytest.raises(grpc.RpcError):
        await failing_rpc(None, context)

    assert context.aborted
    assert context.code == grpc.StatusCode.RESOURCE_EXHAUSTED
    assert "Not enough money" in context.details


@pytest.mark.asyncio
async def test_handle_ccxt_exception_no_context_raises_normally():
    import ccxt

    @handle_ccxt_exception
    async def failing_internal_helper(request):
        raise ccxt.InsufficientFunds("Not enough money")

    # If no context is found, it should just raise the original exception
    with pytest.raises(ccxt.InsufficientFunds):
        await failing_internal_helper(None)


@pytest.mark.asyncio
async def test_batch_isolation_pattern():
    """Verify that using internal helpers in a batch doesn't abort the whole batch if one fails."""
    import ccxt

    class MyConnector:
        async def _impl(self, i):
            if i == 1:
                raise ccxt.ExchangeError("Failure")
            return i

        @handle_ccxt_exception
        async def MyBatchRPC(self, request, context):
            tasks = [self._impl(i) for i in range(3)]
            results = await asyncio.gather(*tasks, return_exceptions=True)
            return results

    connector = MyConnector()
    results = await connector.MyBatchRPC(None, MockContext())

    assert results[0] == 0
    assert isinstance(results[1], ccxt.ExchangeError)
    assert results[2] == 2
