---
title: Fix Code Review Findings
type: fix
date: 2026-02-02
---

# Fix Code Review Findings

## Overview

Address critical findings from the code review of the branch management scripts. Merge `audit_branches.sh` and `cleanup_branches.sh` into a single, robust `scripts/manage_branches.sh` tool.

## Problem Statement

The current implementation has several issues:
1.  **Security**: Potential command injection in `git branch -D` and unsafe JSON construction.
2.  **Reliability**: Squash detection logic is fragile (only checks last 100 commits) and `git for-each-ref` parsing is unsafe.
3.  **Complexity**: Two scripts communicating via JSON creates an unnecessary dependency on `jq`.
4.  **Correctness**: `git for-each-ref` output format is not robust against special characters.

## Proposed Solution

Consolidate logic into `scripts/manage_branches.sh`.

### Features
1.  **Single Entry Point**: Audit and cleanup in one tool.
2.  **Robust Parsing**: Use `\0` (null) or `\x1F` delimiter for `git for-each-ref`.
3.  **Improved Squash Detection**: Check `git cherry` (Tier 2) and `git diff` (Tier 4) in addition to Tree Hash.
4.  **Hardened Security**: Use `--` delimiter for all git commands.
5.  **Removed Dependency**: Internal logic uses Bash arrays; `jq` is only needed if the user *requests* JSON output (which we keep for tests).

## Technical Approach

### Script Structure (`scripts/manage_branches.sh`)

```bash
#!/usr/bin/env bash
set -euo pipefail

# Args: --delete, --force, --json

# 1. Audit Phase
# - Iterate refs with safe delimiter
# - Perform Tier 1-4 checks
# - Store candidates in arrays: MERGED_BRANCHES, SQUASHED_BRANCHES

# 2. Output Phase
# - If --json: Print JSON
# - If ! --json: Print Table

# 3. Cleanup Phase (if --delete)
# - Confirm (unless --force)
# - Loop and delete safely
```

### Safety Improvements
- **Regex Protection**: `PROTECTED_REGEX="^(main|master|dev|develop|staging|release/.*)$"`
- **Optimization**: Fetch target tree hashes once into memory.

## Acceptance Criteria

- [ ] `scripts/manage_branches.sh` exists and is executable.
- [ ] `scripts/audit_branches.sh` and `scripts/cleanup_branches.sh` are removed.
- [ ] Tests (`tests/audit_branches_test.sh`) pass (updated to call new script).
- [ ] No `jq` dependency for normal operation.
- [ ] Command injection proof (`git branch -D -- "$branch"`).

## References

- `docs/solutions/git-patterns/detecting-squash-merges.md`
