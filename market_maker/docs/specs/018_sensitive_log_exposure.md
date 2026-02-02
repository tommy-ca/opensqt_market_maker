# Sensitive Log Exposure Fix Spec

## Problem
The authentication interceptor (`internal/auth/interceptor.go`) logs a masked version of the API key on failure (`apiKey[:8] + "***"`). For a typical 32-char key, this exposes 25% of the secret, reducing the keyspace significantly and violating security compliance standards (PCI DSS, SOC 2).

## Solution
Eliminate logging of any part of the API key. Instead, use:
1.  **Request ID Correlation**: Log a unique Request ID (already good practice) or simply avoid logging the key.
2.  **Context Logging**: Log the client IP, method, and a generic failure message.
3.  **Hash-based Correlation (Optional)**: If debugging is critical, log a SHA-256 hash of the key, but even that is risky if entropy is low. Better to stick to "Invalid Key provided".

## Implementation Plan

### 1. Update `internal/auth/interceptor.go`
Remove the logic that logs `maskAPIKey(apiKey)`.

**Current Code:**
```go
v.failureLogger.Warn("Authentication failed: invalid API key",
    "method", info.FullMethod,
    "key_prefix", maskAPIKey(apiKey))
```

**New Code:**
```go
v.failureLogger.Warn("Authentication failed: invalid API key",
    "method", info.FullMethod,
    // No key data
)
```

Also check for `maskAPIKey` function definition and remove it if unused.

### 2. Audit Codebase
Search for other occurrences of sensitive logging.
Keywords: `apiKey`, `api_key`, `secret`, `password`, `Authorization`.
Tools: `grep` via Bash tool.

### 3. Verify
Create a test case that triggers an auth failure and captures logs (if possible) or just manually verify the code change. Since we can't easily capture logs in a unit test without a custom logger hook, visual inspection and `grep` audit is the primary verification method.

## Acceptance Criteria
- [ ] No part of the API key is logged in `interceptor.go`.
- [ ] `maskAPIKey` function is removed or updated to return constant string (e.g. `[REDACTED]`).
- [ ] Codebase audit confirms no other obvious leaks.
