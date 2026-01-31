module: python-connector
date: 2026-01-30
problem_type:
  - security
  - architecture
  - data_integrity
component: python-connector
symptoms:
  - plaintext gRPC
  - batch side-effects
  - incorrect precision
root_cause: insecure-defaults-and-proto-drift
severity: critical
tags: [grpc, ccxt, security, hardening]
---

# Resolution: Python Connector Security Hardening and Protocol Alignment

## Problem Symptom
The Python gRPC connector for Binance was identified as having several critical architectural and security vulnerabilities:
1. **Unauthenticated Plaintext gRPC**: The server bound to `0.0.0.0` with no encryption or authentication, exposing sensitive trading keys.
2. **Partial Fill Integrity Loss**: Batch operations (`BatchPlaceOrders`) would abort the entire RPC if a single order failed, due to calling unary handlers that utilized `context.abort()`.
3. **Decorator Reliability**: The `handle_ccxt_exception` decorator failed to find the gRPC context when passed as a keyword argument, causing crashes inside the error handler.
4. **Protocol Drift**: Conflicting Protobuf definitions between Go and Python led to wire-type mismatches (e.g., `string` vs `google.type.Decimal`).
5. **Supply Chain Risk**: Explicit dependency on the PyPI `asyncio` package caused conflicts with the modern Python standard library.

## Investigation Steps
- **Code Audit**: Verified `main.py` defaults for server binding and insecure ports.
- **Trace Analysis**: Observed that `BatchPlaceOrders` fallback path called `self.PlaceOrder(req, context)`, which triggered the decorator-level `abort()`.
- **Unit Testing**: Confirmed that `test_errors_robustness.py` failed when `context` was not the last positional argument.
- **Git Archaeology**: Identified tracked PEM keys in the `market_maker` subrepo index using `git ls-files`.

## Root Cause Analysis
The system suffered from "insecure by default" configurations and a lack of separation between the transport layer (gRPC) and business logic. Specifically, the tight coupling of RPC handlers to the gRPC `context` prevented safe parallel execution of operations. Additionally, the lack of a single source of truth for Protobuf definitions allowed the two languages to diverge in their wire formats.

## Working Solution

### 1. Security Hardening
The server now binds to loopback by default and supports TLS and Shared Token authentication.

```python
# main.py
class AuthInterceptor(grpc.aio.ServerInterceptor):
    async def intercept_service(self, continuation, handler_call_details):
        metadatas = dict(handler_call_details.invocation_metadata)
        if metadatas.get("authorization") != f"Bearer {self._token}":
            return self._abort_unauthenticated()
        return await continuation(handler_call_details)

async def serve():
    # ... CLI args for host, tls_cert, auth_token ...
    server = grpc.aio.server(interceptors=interceptors)
    listen_addr = f"{args.host}:{args.port}"
    if args.tls_cert:
        server.add_secure_port(listen_addr, server_credentials)
    else:
        server.add_insecure_port(listen_addr)
```

### 2. Architecture: Internal Implementation Helpers
RPC handlers now delegate to internal methods that do not take a gRPC `context`. This allows batch operations to gather results without a single error aborting the entire call.

```python
# binance.py
async def _place_order_impl(self, req: models_pb2.PlaceOrderRequest) -> models_pb2.Order:
    # Core logic here...
    return self._map_order(order)

@handle_ccxt_exception
async def PlaceOrder(self, request, context):
    return await self._place_order_impl(request)

@handle_ccxt_exception
async def BatchPlaceOrders(self, request, context):
    tasks = [self._place_order_impl(req) for req in request.orders]
    results = await asyncio.gather(*tasks, return_exceptions=True)
    # Collect errors individually and return in BatchPlaceOrdersResponse
```

### 3. Robust Decorator Context Binding
Used `inspect.signature` to dynamically locate the `context` argument.

```python
# errors.py
def _get_grpc_context(func, args, kwargs):
    if "context" in kwargs: return kwargs["context"]
    try:
        sig = inspect.signature(func)
        bound = sig.bind_partial(*args, **kwargs)
        return bound.arguments.get("context")
    except Exception: return None
```

### 4. Protocol Alignment
Consolidated protos in `market_maker/api/proto` and used `google.type.Decimal` for all monetary fields. Updated Batch APIs to return structured error lists (`BatchOrderError`).

## Prevention Strategies
- **Secure by Default**: All connectors must bind to `127.0.0.1` unless explicitly overridden.
- **Internal/Public Split**: Never call a public RPC handler from another handler. Always extract an internal `_impl` method.
- **Unified Generation**: Use a centralized `Makefile` or `buf` command to generate all language bindings from the same source files.
- **Credential Scrutiny**: Add `*.pem` to all `.gitignore` files and use pre-commit hooks to scan for accidentally tracked secrets.
- **Numerical Precision**: Always use string-backed `Decimal` types for exchange values; avoid `float` conversions entirely.
- **Async Resource Management**: Always use `try...finally` blocks or context managers to ensure background tasks and connections are closed, even on error.

## Related Issues
- **See also**: `docs/solutions/architecture-patterns/multi-pair-portfolio-arbitrage-unified-margin-support.md`
- **See also**: `docs/best_practices/arbitrage_margin_prevention.md`
- **See also**: `docs/best_practices/python-connector-prevention.md`
