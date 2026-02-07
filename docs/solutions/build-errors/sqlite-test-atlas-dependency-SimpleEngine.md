---
title: "Atlas Dependency Failure in SimpleEngine SQLite Tests"
date: 2026-02-07
status: resolved
severity: medium
category: build-errors
tags: [sqlite, build-failure, dependencies, atlas, simple-engine, go-test]
related_issues: []
---

# Atlas Dependency Failure in SimpleEngine SQLite Tests

## Problem Statement

The test suite for `SimpleEngine` failed to run in the CI/CD environment and on some developer machines. The failure occurred during the test setup phase when initializing the SQLite database schema.

### Symptoms
- `go test ./...` fails with an error.
- Error message: `exec: "atlas": executable file not found in $PATH` or `atlas: command not found`.
- Tests dependent on database migrations cannot start.
- Requires manual installation of the `atlas` CLI tool to run unit tests, which breaks the "zero-setup" development principle.

## Investigation & Findings

### Root Cause Analysis
The test setup code was using `os/exec` to shell out to the `atlas` command-line tool to apply migrations to the ephemeral SQLite test database.

```go
// Before: Relying on external binary
cmd := exec.Command("atlas", "schema", "apply", "--url", dbUrl, "--to", "file://schema.hcl")
if err := cmd.Run(); err != nil {
    return fmt.Errorf("failed to run migrations: %w", err)
}
```

This introduced a hidden system dependency. If the environment (CI container or new dev machine) didn't have `atlas` installed, the tests crashed. Unit tests should generally be self-contained and not rely on external system binaries.

## Solution

Removed the dependency on the external `atlas` CLI by embedding the schema creation directly into the Go test code using standard SQL.

### Implementation Details
Replaced the shell command with `db.Exec`. Since `SimpleEngine` tests use a lightweight SQLite instance, we can simply execute the `CREATE TABLE` statements directly.

```go
// After: Inline SQL schema definition
schema := `
CREATE TABLE IF NOT EXISTS orders (
    id TEXT PRIMARY KEY,
    symbol TEXT NOT NULL,
    side TEXT NOT NULL,
    price REAL,
    quantity REAL,
    status TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS positions (
    symbol TEXT PRIMARY KEY,
    quantity REAL,
    average_price REAL
);
`

_, err := db.Exec(schema)
if err != nil {
    t.Fatalf("Failed to create schema: %v", err)
}
```

This ensures that:
1. Tests run anywhere Go is installed.
2. No external tools need to be downloaded or configured.
3. Test execution time is faster (no process spawning).

## Prevention & Best Practices

### 1. Hermetic Tests
Tests should be hermetic. Avoid shelling out to system commands unless testing the CLI interaction itself.

### 2. Embed Schema for Tests
For unit tests using SQLite, prefer hardcoded `CREATE TABLE` strings or use Go migration libraries (like `golang-migrate` or `goose`) that can be imported as libraries, rather than calling CLI tools.

### 3. Check Dependencies in Makefile
If a tool is absolutely required, the `Makefile` or `setup` script should check for its existence and install it, but purely code-based solutions are preferred for libraries.
