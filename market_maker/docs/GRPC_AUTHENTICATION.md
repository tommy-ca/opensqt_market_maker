# gRPC Authentication Guide

## Overview

The gRPC server now supports API key-based authentication to protect all RPC endpoints from unauthorized access. This document describes how to configure and use the authentication system.

## Authentication Method

**API Key Authentication** - Simple and effective authentication using API keys passed in gRPC metadata.

### Features

- **Unary and Stream RPC Protection**: All 23 RPC methods are protected (both unary and streaming)
- **Rate Limiting**: Per-key rate limiting to prevent abuse (default: 100 requests/second)
- **Audit Logging**: All authentication failures are logged for security monitoring
- **Key Rotation**: Support for adding/removing API keys without server restart
- **TLS Compatible**: Works seamlessly with TLS encryption for defense-in-depth security

## Configuration

### Server Configuration

Add API keys to your `config.yaml`:

```yaml
exchanges:
  remote:
    base_url: "localhost:50051"
    tls_cert_file: "certs/server-cert.pem"
    tls_key_file: "certs/server-key.pem"
    tls_server_name: "localhost"
    grpc_api_keys: "${GRPC_API_KEYS}"  # Comma-separated API keys
    grpc_rate_limit: 100                # Requests per second per key
```

### Environment Variables

Set the API keys in your environment (recommended over config files):

```bash
# Server: Multiple API keys (comma-separated)
export GRPC_API_KEYS="prod-key-1,prod-key-2,prod-key-3"

# Client: Single API key
export GRPC_API_KEY="prod-key-1"
```

**Security Best Practices:**
- Use strong, randomly generated API keys (minimum 32 characters)
- Store API keys in environment variables, NOT in config files
- Rotate keys regularly (at least every 90 days)
- Use different keys for different environments (dev, staging, prod)
- Never commit API keys to version control

### Generating Secure API Keys

```bash
# Generate a secure random API key (Linux/macOS)
openssl rand -base64 32

# Or using uuidgen
uuidgen | tr -d '-' | tr '[:upper:]' '[:lower:]'
```

## Server Implementation

### Option 1: Server with Authentication (Recommended)

```go
import (
    "market_maker/internal/exchange"
    "market_maker/internal/logging"
)

// Load configuration
config, err := config.LoadConfig("configs/config.yaml")
if err != nil {
    log.Fatal(err)
}

// Parse API keys from config
exchangeConfig := config.Exchanges["remote"]
apiKeys := exchangeConfig.ParseGRPCAPIKeys()
rateLimit := exchangeConfig.GetGRPCRateLimit()

// Create logger
logger := logging.NewLogger(logging.InfoLevel, nil)

// Create server WITH authentication
server := exchange.NewExchangeServerWithAuth(
    exchangeInstance,
    logger,
    apiKeys,
    rateLimit,
)

// Start with TLS and authentication
err = server.StartWithTLS(50051, certFile, keyFile)
```

### Option 2: Server without Authentication (Backward Compatible)

```go
// Create server WITHOUT authentication (insecure)
server := exchange.NewExchangeServer(exchangeInstance, logger)

// Start without authentication (logs warning)
err = server.Start(50051)
```

## Client Implementation

### Option 1: Client with Authentication (Recommended)

```go
import (
    "market_maker/internal/exchange"
    "market_maker/internal/logging"
)

logger := logging.NewLogger(logging.InfoLevel, nil)

// Load API key from environment
apiKey := os.Getenv("GRPC_API_KEY")

// Connect with TLS and API key authentication
client, err := exchange.NewRemoteExchangeWithTLSAndAuth(
    "localhost:50051",
    logger,
    "certs/server-cert.pem",
    "localhost",
    apiKey,
)
```

### Option 2: Client without Authentication

```go
// Connect without authentication (will fail if server requires auth)
client, err := exchange.NewRemoteExchangeWithTLS(
    "localhost:50051",
    logger,
    "certs/server-cert.pem",
    "localhost",
)
```

## Authentication Flow

1. **Client Request**: Client adds API key to gRPC metadata header `x-api-key`
2. **Server Intercepts**: Server interceptor extracts and validates API key
3. **Rate Limit Check**: Server checks if request is within rate limit
4. **Request Processing**: If valid, request proceeds to handler
5. **Audit Logging**: All authentication failures are logged

### Metadata Format

```go
import "google.golang.org/grpc/metadata"

// Client automatically adds metadata using addAuthMetadata():
md := metadata.Pairs("x-api-key", apiKey)
ctx = metadata.NewOutgoingContext(ctx, md)
```

## Rate Limiting

Each API key has an independent rate limit (token bucket algorithm):

- **Default**: 100 requests per second
- **Configurable**: Set via `grpc_rate_limit` in config
- **Per-Key**: Each API key has its own rate limit bucket
- **Token Refill**: Tokens refill continuously at configured rate
- **Overflow**: Excess requests return `ResourceExhausted` error

## Error Handling

### Authentication Errors

| Error Code | Reason | Solution |
|------------|--------|----------|
| `Unauthenticated` | Missing metadata | Add API key to client config |
| `Unauthenticated` | Missing API key | Set `x-api-key` in metadata |
| `Unauthenticated` | Invalid API key | Check API key matches server config |
| `ResourceExhausted` | Rate limit exceeded | Reduce request rate or increase limit |

### Client-Side Error Handling

```go
resp, err := client.PlaceOrder(ctx, req)
if err != nil {
    if st, ok := status.FromError(err); ok {
        switch st.Code() {
        case codes.Unauthenticated:
            log.Error("Authentication failed - check API key")
        case codes.ResourceExhausted:
            log.Warn("Rate limit exceeded - backing off")
            time.Sleep(time.Second)
        }
    }
}
```

## Key Rotation

### Adding a New API Key

```go
// Server-side (runtime key rotation)
validator.AddAPIKey("new-prod-key-4")
```

### Removing an Old API Key

```go
// Server-side (runtime key rotation)
validator.RemoveAPIKey("old-key-to-retire")
```

### Recommended Rotation Process

1. **Generate new key**: Create strong random key
2. **Add to server**: Add new key while keeping old key active
3. **Update clients**: Gradually migrate clients to new key
4. **Monitor**: Verify all clients using new key
5. **Remove old key**: After grace period, remove old key from server

## Security Considerations

### Defense in Depth

API key authentication is **Layer 2** of security:

1. **Layer 1 (Required)**: TLS encryption (protects keys in transit)
2. **Layer 2 (This)**: API key authentication (protects endpoints)
3. **Layer 3 (Future)**: JWT with fine-grained permissions

### Limitations

- **No per-method permissions**: All valid keys have access to all methods
- **Stateless validation only**: No session management or token expiry
- **Key compromise**: If key leaks, must be rotated immediately

### Migration to JWT (Recommended for Production)

For production deployments with multiple clients and complex permission requirements, consider migrating to JWT:

- **Token expiry**: Automatic key rotation via short-lived tokens
- **Claims-based permissions**: Fine-grained access control per RPC method
- **User attribution**: Track which user/service made each request
- **Revocation**: Immediate token revocation without server restart

## Testing

### Unit Tests

```bash
# Run authentication tests
go test ./internal/auth/... -v
```

### Integration Tests

```bash
# Test with authentication enabled
export GRPC_API_KEYS="test-key-1,test-key-2"
export GRPC_API_KEY="test-key-1"
go test ./internal/exchange/... -v
```

### Manual Testing

```bash
# Start server with authentication
export GRPC_API_KEYS="test-key-123"
./exchange_connector

# Test with valid key
grpcurl -H "x-api-key: test-key-123" \
  -d '{"symbol": "BTCUSDT"}' \
  localhost:50051 \
  opensqt.market_maker.v1.ExchangeService/GetLatestPrice

# Test with invalid key (should fail)
grpcurl -H "x-api-key: wrong-key" \
  -d '{"symbol": "BTCUSDT"}' \
  localhost:50051 \
  opensqt.market_maker.v1.ExchangeService/GetLatestPrice
```

## Monitoring and Audit

### Authentication Failure Logs

All authentication failures are logged with:
- Timestamp
- RPC method attempted
- Masked API key (first 8 characters)
- Failure reason

Example log output:
```
[2026-01-23 10:15:32.123] [WARN] Authentication failed: invalid API key {method=/opensqt.market_maker.v1.ExchangeService/PlaceOrder, key_prefix=abcdefgh***, component=auth_failure}
```

### Rate Limit Logs

```
[2026-01-23 10:15:45.678] [WARN] Rate limit exceeded {method=/opensqt.market_maker.v1.ExchangeService/GetLatestPrice, key_prefix=prodkey1***, component=auth_failure}
```

### Monitoring Recommendations

1. **Alert on repeated auth failures**: May indicate attack or misconfiguration
2. **Track rate limit violations**: May indicate client bugs or DDoS
3. **Monitor key usage patterns**: Detect anomalous behavior per key
4. **Regular key rotation audits**: Ensure keys are rotated on schedule

## Troubleshooting

### Problem: "missing metadata" error

**Cause**: Client not sending metadata
**Solution**: Ensure using `NewRemoteExchangeWithAuth()` or `NewRemoteExchangeWithTLSAndAuth()`

### Problem: "invalid API key" error

**Cause**: API key mismatch between client and server
**Solution**:
1. Check `GRPC_API_KEYS` on server includes the key
2. Check `GRPC_API_KEY` on client matches server key
3. Verify no whitespace in environment variables

### Problem: "rate limit exceeded" error

**Cause**: Too many requests from client
**Solution**:
1. Reduce request rate
2. Implement client-side backoff
3. Increase `grpc_rate_limit` in server config

## References

- [gRPC Authentication Guide](https://grpc.io/docs/guides/auth/)
- [gRPC Metadata](https://grpc.io/docs/guides/metadata/)
- [TODO #002 - gRPC Authentication](../todos/002-pending-p1-no-grpc-authentication.md)
- [TODO #001 - TLS Implementation](../todos/001-resolved-p1-no-grpc-tls.md)
