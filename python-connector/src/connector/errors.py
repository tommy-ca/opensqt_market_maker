import asyncio
import ccxt
import grpc
import functools
import logging
import inspect

logger = logging.getLogger(__name__)


def _get_grpc_context(func, args, kwargs):
    """Robustly find the gRPC context in the arguments."""
    # 1. Check keyword arguments
    if "context" in kwargs:
        return kwargs["context"]

    # 2. Check positional arguments by binding signature
    try:
        sig = inspect.signature(func)
        bound = sig.bind_partial(*args, **kwargs)
        if "context" in bound.arguments:
            return bound.arguments["context"]
    except Exception:
        pass

    # 3. Last-ditch: find anything with an .abort method
    for arg in args:
        if hasattr(arg, "abort") and callable(getattr(arg, "abort")):
            return arg

    return None


EXCEPTION_MAP = [
    (ccxt.InsufficientFunds, grpc.StatusCode.RESOURCE_EXHAUSTED),
    (ccxt.OrderNotFound, grpc.StatusCode.NOT_FOUND),
    (ccxt.DuplicateOrderId, grpc.StatusCode.ALREADY_EXISTS),
    (ccxt.InvalidOrder, grpc.StatusCode.INVALID_ARGUMENT),
    (ccxt.BadRequest, grpc.StatusCode.INVALID_ARGUMENT),
    (ccxt.AuthenticationError, grpc.StatusCode.UNAUTHENTICATED),
    (ccxt.RateLimitExceeded, grpc.StatusCode.RESOURCE_EXHAUSTED),
    (ccxt.NetworkError, grpc.StatusCode.UNAVAILABLE),
    (ccxt.ExchangeNotAvailable, grpc.StatusCode.UNAVAILABLE),
    (ccxt.ExchangeError, grpc.StatusCode.FAILED_PRECONDITION),
]


def handle_ccxt_exception(func):
    @functools.wraps(func)
    async def wrapper(*args, **kwargs):
        try:
            return await func(*args, **kwargs)
        except Exception as e:
            # Avoid re-wrapping if it's already a gRPC error from a nested call
            # but usually we want to map CCXT specifically.
            if isinstance(e, asyncio.CancelledError):
                raise

            context = _get_grpc_context(func, args, kwargs)

            status_code = grpc.StatusCode.UNKNOWN
            for exc_class, code in EXCEPTION_MAP:
                if isinstance(e, exc_class):
                    status_code = code
                    break

            if status_code == grpc.StatusCode.UNKNOWN:
                logger.exception(f"Unhandled exception in {func.__name__}: {e}")
            else:
                logger.warning(
                    f"CCXT exception in {func.__name__} mapped to {status_code}: {e}"
                )

            if context:
                # context.abort in grpc.aio raises an exception.
                await context.abort(status_code, str(e))

            raise

    return wrapper


def retry_transient(max_retries=3, initial_backoff=0.1):
    def decorator(func):
        @functools.wraps(func)
        async def wrapper(*args, **kwargs):
            backoff = initial_backoff
            for attempt in range(max_retries + 1):
                try:
                    return await func(*args, **kwargs)
                except (
                    ccxt.NetworkError,
                    ccxt.ExchangeNotAvailable,
                    ccxt.RateLimitExceeded,
                ) as e:
                    if attempt == max_retries:
                        raise

                    logger.warning(
                        f"Transient error in {func.__name__} (attempt {attempt + 1}/{max_retries + 1}): {e}. Retrying in {backoff}s..."
                    )
                    await asyncio.sleep(backoff)
                    backoff *= 2  # Exponential backoff

            return None  # Should not be reached

        return wrapper

    return decorator
