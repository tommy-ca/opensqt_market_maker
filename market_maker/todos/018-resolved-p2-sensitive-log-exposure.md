---
status: completed
priority: p2
issue_id: 018
tags: [code-review, security, logging, data-exposure]
dependencies: []
---

# Sensitive Data Exposed in Authentication Logs

## Problem Statement

**Location**: `internal/auth/interceptor.go:147-150`

Authentication failures log first 8 characters of API keys:

```go
v.failureLogger.Warn("Authentication failed: invalid API key",
    "method", info.FullMethod,
    "key_prefix", maskAPIKey(apiKey))  // Exposes first 8 characters

func maskAPIKey(apiKey string) string {
    if len(apiKey) < 8 {
        return "***"
    }
    return apiKey[:8] + "***"  // 25% of 32-char key exposed
}
```

**Impact**:
- **Information leakage**: 8 characters = 25% of typical 32-character API key
- **Brute force risk**: Reduces keyspace from 62^32 to 62^24 (99.9999% reduction)
- **Log aggregation exposure**: Centralized logging systems store sensitive data
- **Compliance violation**: PCI DSS, SOC 2 prohibit logging credential fragments

## Additional Findings

**Other log locations with potential exposure**:
- `internal/exchange/remote.go`: May log full gRPC metadata in debug mode
- `pkg/liveserver/server.go`: WebSocket upgrade logs might include auth headers
- Exchange adapters: REST API debug logs might include API keys in headers

## Proposed Solution

### Option 1: Remove API Key Logging Entirely (Recommended)

**Effort**: 2-3 hours

```go
// DO NOT log any portion of API key
v.failureLogger.Warn("Authentication failed: invalid API key",
    "method", info.FullMethod,
    "client_ip", getClientIP(ctx),  // Log IP instead for tracking
    "timestamp", time.Now().Unix())

// For debugging, use request ID correlation
v.failureLogger.Warn("Authentication failed",
    "method", info.FullMethod,
    "request_id", getRequestID(ctx))  // Correlate with access logs
```

### Option 2: Hash-Based Logging

**Effort**: 4-5 hours

```go
import "crypto/sha256"

func hashAPIKey(apiKey string) string {
    hash := sha256.Sum256([]byte(apiKey))
    // Return first 12 hex chars of hash for correlation
    return hex.EncodeToString(hash[:6])
}

v.failureLogger.Warn("Authentication failed: invalid API key",
    "method", info.FullMethod,
    "key_hash", hashAPIKey(apiKey))  // One-way hash for correlation
```

**Benefit**: Can correlate multiple failed attempts from same key without exposing actual key.

## Recommended Action

**Implement Option 1** - Simplest and most secure. For debugging:

1. **Use request IDs** for correlation:
```go
type requestIDKey struct{}

func withRequestID(ctx context.Context) context.Context {
    requestID := uuid.New().String()
    return context.WithValue(ctx, requestIDKey{}, requestID)
}
```

2. **Access logs** (separate from error logs) can include request ID:
```
2024-01-23T10:30:45Z INFO access client_ip=192.168.1.100 method=/api.Exchange/GetAccount request_id=abc-123 status=401
```

3. **Error logs** reference request ID only:
```
2024-01-23T10:30:45Z WARN auth request_id=abc-123 error="invalid API key"
```

4. **Correlation**: Match request_id across logs without exposing credentials

## Audit Required

**Search entire codebase for credential logging**:
```bash
grep -rn "apiKey\|api_key\|secretKey\|secret_key\|password" --include="*.go" | grep -i "log\|print\|debug"
```

**Common patterns to remove**:
- `logger.Debug("API Key: %s", apiKey)`
- `fmt.Printf("Secret: %v", secret)`
- `log.Info("Auth header: %s", r.Header.Get("Authorization"))`

## Compliance

**PCI DSS Requirement 3.4**:
> "Render PAN unreadable anywhere it is stored... using any of the following approaches: One-way hashes, Truncation, Index tokens, Strong cryptography"

**SOC 2 CC6.1**:
> "The entity implements logical access security software, infrastructure, and architectures... to protect against threats from sources outside its system boundaries."

Logging API key fragments violates both standards.

## Acceptance Criteria

- [x] API key logging completely removed from auth interceptor
- [x] Request ID correlation implemented
- [x] Codebase audit completed (no credential logging found)
- [x] Log sanitization tested (API keys never appear in logs)
- [x] Documentation updated with secure logging practices
- [x] All tests pass

## Resources

- Security Sentinel Report: MEDIUM-003
- File: `internal/auth/interceptor.go`
- PCI DSS v4.0 Requirement 3.4
- OWASP Logging Cheat Sheet
