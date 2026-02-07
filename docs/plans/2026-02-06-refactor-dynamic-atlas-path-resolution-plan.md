---
title: Dynamic Atlas Path Resolution
type: refactor
date: 2026-02-06
---

# Dynamic Atlas Path Resolution

## Overview

Update `market_maker/internal/engine/simple/store_sqlite_test.go` to resolve the `atlas` migration tool dynamically using `exec.LookPath` (equivalent to `which`) and resolve the migration directory relative to the test file. This replaces hardcoded paths and manual SQL schema creation, ensuring tests run correctly across different environments where `atlas` is installed.

## Problem Statement

1.  **Hardcoded Binary Path**: The previous implementation used a hardcoded path (`/home/tommyk/.local/...`) for `atlas`, causing failures when the tool location changed or on different machines.
2.  **Hardcoded Migration Path**: The migration directory URL was also hardcoded (`file:/home/tommyk/...`), making the test fragile.
3.  **Temporary Workaround**: We currently bypass `atlas` by manually executing a `CREATE TABLE` string. This drifts from production behavior where `atlas` is the source of truth for schema management.

## Proposed Solution

Restore the `atlas migrate apply` logic in tests but make it robust:
1.  **Find Atlas**: Use `exec.LookPath("atlas")` to locate the binary in `$PATH`.
2.  **Find Migrations**: Use `filepath.Abs("../../migrations")` to locate the schema files relative to the test package.
3.  **Execution**: Run the migration command using these dynamic paths.

## Acceptance Criteria

- [ ] `market_maker/internal/engine/simple/store_sqlite_test.go` uses `exec.LookPath("atlas")`.
- [ ] Migration directory is resolved dynamically (no hardcoded `/home/tommyk`).
- [ ] Tests pass when `atlas` is in `$PATH`.
- [ ] Tests fail with a clear error if `atlas` is missing (or skip, depending on preference, but failure ensures env correctness).

## Implementation Details

### `createTestStore` Refactor

```go
func createTestStore(t *testing.T, dbPath string) *SQLiteStore {
    // 1. Resolve Atlas Binary
    atlasPath, err := exec.LookPath("atlas")
    if err != nil {
        t.Fatalf("atlas CLI not found in PATH: %v. Please install atlas.", err)
    }

    // 2. Resolve Migration Directory
    // Test runs in market_maker/internal/engine/simple
    // Migrations are in market_maker/migrations
    relPath := "../../../migrations"
    absDir, err := filepath.Abs(relPath)
    if err != nil {
        t.Fatalf("failed to resolve migration dir: %v", err)
    }
    dirURL := "file://" + absDir

    // 3. Run Migration
    cmd := exec.Command(atlasPath, "migrate", "apply",
        "--dir", dirURL,
        "--url", "sqlite://"+dbPath,
    )
    
    if out, err := cmd.CombinedOutput(); err != nil {
        t.Fatalf("failed to apply migrations: %v\nOutput: %s", err, out)
    }

    store, err := NewSQLiteStore(dbPath)
    if err != nil {
        t.Fatalf("failed to create store: %v", err)
    }
    return store
}
```

## References

-   `market_maker/internal/engine/simple/store_sqlite_test.go`
