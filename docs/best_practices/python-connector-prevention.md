# Python Connector Prevention Strategies & Best Practices

This document outlines the strategies developed to prevent common issues in the Python gRPC connectors.

## 1. Security: Secure by Default

*   **Loopback Bindings:** The gRPC server MUST default to binding to `127.0.0.1`. Binding to `0.0.0.0` or other non-loopback addresses SHOULD be explicitly enabled and REQUIRES an authentication token.
*   **Mandatory Authentication:** All private RPCs (those involving trading, account access, or sensitive data) MUST be protected by a shared secret (Bearer Token).
*   **Auth Interceptor:** Use a `ServerInterceptor` to enforce authentication.
    *   **Public/Private Split:** Health checks and public data (e.g., symbols, some market data) can be exempted from auth if necessary, but private RPCs MUST check the `authorization` metadata.
    *   **Fail Fast:** If an auth token is required but missing or incorrect, abort with `UNAUTHENTICATED`.

## 2. Architecture: Context Isolation

*   **Internal Helpers (`_impl`):** Every public RPC handler MUST have a corresponding internal helper method (e.g., `_place_order_impl`).
*   **Side Effect Prevention:** The public RPC handler handles gRPC context (e.g., `context.abort`). The internal helper SHOULD NOT take the gRPC context.
*   **Batch Operations:** Batch RPCs MUST iterate over internal helpers and use `asyncio.gather(..., return_exceptions=True)` to ensure that a single failure doesn't abort the entire batch or trigger unintended gRPC-wide side effects.

## 3. Reliability: Decorator Robustness

*   **Signature Awareness:** gRPC decorators (like `@handle_ccxt_exception`) MUST be robust to various method signatures (positional vs. keyword arguments) when searching for the gRPC `context`.
*   **Unit Testing Decorators:** Unit tests MUST verify that decorators correctly find the context in all supported signature patterns.
*   **Exception Mapping:** Maintain a strict mapping from library-specific exceptions (e.g., `ccxt.InsufficientFunds`) to gRPC status codes (e.g., `RESOURCE_EXHAUSTED`).

## 4. Supply Chain: Shadowing Audit

*   **Dependency Review:** Periodically audit `requirements.txt` and lockfiles for packages that might shadow the Python Standard Library or common internal modules.
*   **Lockfile Enforcement:** Use lockfiles (e.g., `uv.lock`, `poetry.lock`) to ensure reproducible builds and prevent malicious dependency drift.
*   **Namespace Protection:** Avoid generic names for internal packages to prevent "dependency confusion" attacks.

## 5. Protos: Single Source of Truth

*   **Centralized Protos:** All `.proto` files reside in a single location (`market_maker/api/proto`).
*   **Unified Generation:** Use a tool like `buf` to manage proto generation for all supported languages (Go, Python) from the same source.
*   **Check-in Policy:** Decouple proto definitions from implementation. Protos define the contract; implementation must adhere to it.

## Suggested Test Cases for New Connectors

1.  **Auth Mandatory:** Verify that calling a private RPC without a token returns `UNAUTHENTICATED`.
2.  **Loopback Check:** Verify that the server fails to start if bound to a public IP without an auth token.
3.  **Batch Resilience:** Verify that in a `BatchPlaceOrders` call, if one order fails (e.g., Insufficient Funds), the other valid orders in the batch are still processed (if the exchange supports it) or at least their results are returned.
4.  **Decorator Signature Test:** Verify `handle_ccxt_exception` with:
    *   `async def RPC(self, request, context)`
    *   `async def RPC(self, request, context=None)`
    *   `async def RPC(self, **kwargs)`
5.  **Shadowing Check:** Run a script to list all installed package names and compare them against a list of Python Standard Library modules.
