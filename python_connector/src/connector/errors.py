import asyncio
import ccxt
import grpc
import functools
import traceback
import time


def handle_ccxt_exception(func):
    @functools.wraps(func)
    async def wrapper(self, *args, **kwargs):
        try:
            return await func(self, *args, **kwargs)
        except ccxt.InsufficientFunds as e:
            context = args[-1] if args else None
            if context:
                await context.abort(grpc.StatusCode.RESOURCE_EXHAUSTED, str(e))
            raise
        except ccxt.OrderNotFound as e:
            context = args[-1] if args else None
            if context:
                await context.abort(grpc.StatusCode.NOT_FOUND, str(e))
            raise
        except ccxt.DuplicateOrderId as e:
            context = args[-1] if args else None
            if context:
                await context.abort(grpc.StatusCode.ALREADY_EXISTS, str(e))
            raise
        except (ccxt.InvalidOrder, ccxt.BadRequest) as e:
            context = args[-1] if args else None
            if context:
                await context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
            raise
        except ccxt.AuthenticationError as e:
            context = args[-1] if args else None
            if context:
                await context.abort(grpc.StatusCode.UNAUTHENTICATED, str(e))
            raise
        except ccxt.RateLimitExceeded as e:
            context = args[-1] if args else None
            if context:
                await context.abort(grpc.StatusCode.RESOURCE_EXHAUSTED, str(e))
            raise
        except (ccxt.NetworkError, ccxt.ExchangeNotAvailable) as e:
            context = args[-1] if args else None
            if context:
                await context.abort(grpc.StatusCode.UNAVAILABLE, str(e))
            raise
        except ccxt.ExchangeError as e:
            context = args[-1] if args else None
            if context:
                await context.abort(grpc.StatusCode.FAILED_PRECONDITION, str(e))
            raise
        except Exception as e:
            print(f"Unhandled exception in {func.__name__}: {e}")
            traceback.print_exc()
            context = args[-1] if args else None
            if context:
                await context.abort(grpc.StatusCode.UNKNOWN, str(e))
            raise

    return wrapper


def retry_transient(max_retries=3, initial_backoff=0.1):
    def decorator(func):
        @functools.wraps(func)
        async def wrapper(self, *args, **kwargs):
            last_err = None
            backoff = initial_backoff
            for attempt in range(max_retries + 1):
                try:
                    return await func(self, *args, **kwargs)
                except (
                    ccxt.NetworkError,
                    ccxt.ExchangeNotAvailable,
                    ccxt.RateLimitExceeded,
                ) as e:
                    last_err = e
                    if attempt == max_retries:
                        break

                    # Log retry attempt
                    print(
                        f"Transient error in {func.__name__} (attempt {attempt + 1}/{max_retries + 1}): {e}. Retrying in {backoff}s..."
                    )
                    await asyncio.sleep(backoff)
                    backoff *= 2  # Exponential backoff

            raise last_err

        return wrapper

    return decorator
