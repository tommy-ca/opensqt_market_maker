---
title: "Config Secret Masking Leak"
date: 2026-02-07
status: resolved
severity: critical
category: security-issues
tags: [security, logging, secrets, pii, config]
related_issues: []
---

# Config Secret Masking Leak

## Problem Statement

The application's configuration logging mechanism was inadvertently leaking information about secrets (API keys, passwords). While it attempted to mask them, the implementation revealed the length of the secret and potentially the suffix, reducing the search space for an attacker.

### Symptoms
- Logs contained entries like `API_KEY=********************a1b2` or `PASSWORD=******` (matching actual length).
- Security audit tools flagged "Potential secret exposure in logs".
- Log files revealed the exact length of credentials, which helps crack hashes or identify key types.

## Investigation & Findings

### Root Cause Analysis
The `maskString` helper function was attempting to be "helpful" by keeping the last few characters visible for debugging purposes (to verify which key was being used) and preserving the string length.

```go
// Vulnerable Implementation
func maskString(s string) string {
    if len(s) <= 4 {
        return "****"
    }
    // Retains length and suffix!
    return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}
```

This is dangerous because:
1. **Length Leaks:** Knowing a password is exactly 8 characters vs 64 characters drastically changes brute-force time.
2. **Suffix Leaks:** If a key rotation fails and the old and new keys share a suffix, this might mislead debugging or leak partial info.

## Solution

The `maskString` function was hardened to return a constant-length, fully opaque placeholder.

### Implementation Details
Updated the function to ignore the input content entirely (except for empty checks) and return a fixed static string.

```go
// Secure Implementation
func maskString(s string) string {
    if s == "" {
        return "[EMPTY]"
    }
    return "********" // Fixed length, no suffix
}
```

Now, whether the API key is 10 characters or 100 characters, the log output is identical: `API_KEY=********`.

## Prevention & Best Practices

### 1. Constant Time/Length Responses
When handling secrets, outputs should be constant in nature to avoid side-channel leaks (timing or length).

### 2. Never Log Secrets (Even Masked)
Ideally, don't log the config struct values at all. If you must, use a whitelist of safe fields rather than a blacklist of secret fields.

### 3. Use Secret Managers
Avoid passing secrets in basic env vars if possible; use secret managers where the application retrieves them directly, reducing the chance they sit in process environment dumps.

### 4. Structural Logging with Redaction
Use structured logging libraries (like `zap` or `slog`) with explicit `Redacted` types that automatically handle JSON marshaling safely.
