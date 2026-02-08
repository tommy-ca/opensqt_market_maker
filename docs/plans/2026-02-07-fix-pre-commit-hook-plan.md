---
title: Fix Pre-Commit Hook Execution
type: fix
date: 2026-02-07
---

# Fix Pre-Commit Hook Execution

## Overview

The current `.git/hooks/pre-commit` script is failing because it attempts to execute a hardcoded binary path (`/home/tommyk/.local/bin/pre-commit`) or relies on a global `pre-commit` installation that is broken or missing. We will replace this with a robust hook script that uses `uvx` (from the `uv` package manager) to execute `pre-commit` ephemerally, ensuring reproducibility across environments without requiring manual global package installation.

## Problem Statement

*   **Error:** `.git/hooks/pre-commit: line 16: /home/tommyk/.local/bin/pre-commit: cannot execute: required file not found`
*   **Root Cause:** The hook script was generated with an absolute path to a specific python interpreter or binary that no longer exists or is invalid.
*   **Goal:** Use `uvx pre-commit` to run hooks, which downloads and runs `pre-commit` on demand if not present, leveraging the project's `uv` toolchain.

## Proposed Solution

1.  **Create a wrapper script** `scripts/run_pre_commit.sh` that attempts to run `pre-commit` via `uvx` first, falling back to other methods.
2.  **Update the git hook** to invoke this wrapper script.
3.  **Specs Driven Development:**
    *   Create a spec defining the hook behavior.
    *   Create a test that verifies the hook runs successfully (mocking the actual commit).

## Implementation Plan

### Phase 1: Specification & Test
1.  Create `docs/specs/pre_commit_hook_spec.md`.
2.  Create `scripts/tests/test_pre_commit_wrapper.sh` to verify the wrapper logic.

### Phase 2: Implementation
1.  Write `scripts/run_pre_commit.sh`.
2.  Update `.git/hooks/pre-commit` to use the new script.

### Phase 3: Verification
1.  Run the test script.
2.  Attempt a dummy commit to verify the hook triggers.

## Acceptance Criteria
- [ ] `scripts/run_pre_commit.sh` exists and uses `uvx`.
- [ ] `.git/hooks/pre-commit` calls the wrapper.
- [ ] `git commit` does not fail with "file not found".

## References
- `uv` documentation on `uvx`
