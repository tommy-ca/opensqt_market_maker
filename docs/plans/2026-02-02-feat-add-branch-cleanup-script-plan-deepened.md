---
title: Add Branch Cleanup Script
type: feat
date: 2026-02-02
---

# Add Branch Cleanup Script

## Enhancement Summary

**Deepened on:** 2026-02-02
**Sections enhanced:** 3
**Research agents used:** Security Sentinel, Code Simplicity Reviewer, Agent-Native Reviewer, Bash Best Practices

### Key Improvements
1.  **Agent Compatibility**: Added `--yes` flag to bypass interactive prompts for AI agents.
2.  **Security Hardening**: Added explicit "Shield Logic" to prevent deleting critical branches (main, master, dev) even if the audit script flags them.
3.  **Command Safety**: Switched to `git branch -D -- "$branch"` to prevent command injection from malicious branch names.
4.  **Robust Output**: Separated logs (stderr) from data (stdout) to ensure composability.

## Overview

Add `scripts/cleanup_branches.sh` to provide an interactive, safe workflow for deleting merged and squashed branches. This script consumes the JSON output from the existing `scripts/audit_branches.sh`.

## Problem Statement

Developers accumulate local branches that have already been merged. While `audit_branches.sh` identifies them, deleting them manually one by one is tedious and error-prone. There is no existing tool to safely batch-delete branches based on "Squash" detection.

## Proposed Solution

Create a bash script that pipelines the audit result into a deletion workflow.

### Features
1.  **Dependency Check**: Verifies `jq` and `audit_branches.sh` exist.
2.  **Filtering**: Selects only branches with status `merged` or `squashed`.
3.  **Safety**:
    *   Excludes current branch.
    *   Displays candidates in a clear table (Human mode).
    *   Requires explicit `y` confirmation (unless `--yes` used).
    *   Hardcoded protection for `main`, `master`, `dev`, `staging`, `production`.
4.  **Execution**: Uses `git branch -D` (Force Delete) because squash-merged branches often fail the standard `-d` check despite being safe to delete.

## Technical Approach

### Script Logic (`scripts/cleanup_branches.sh`)

#### Research Insights

**Security Best Practices:**
- Never trust input branch names implicitly.
- Use `--` delimiter for git commands.
- Check branch SHA before deleting (TOCTOU mitigation).

**Agent-Native Pattern:**
- Interactive prompts block agents. Provide `--yes` flag for autonomous usage.
- Use stderr for progress messages so stdout remains clean for piping.

**Simplicity:**
- Leverage `jq` for filtering and formatting to reduce bash complexity.

#### Implementation Details

```bash
#!/usr/bin/env bash
set -euo pipefail

# ... standard header ...

FORCE_YES=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        -y|--yes) FORCE_YES=true; shift ;;
        *) echo "Unknown option: $1" >&2; exit 1 ;;
    esac
done

# ... run audit ...

# Filter Logic (jq)
# Must explicitly exclude protected branches even if audit says they are "merged"
PROTECTED_REGEX="^(main|master|dev|production|staging)$"

# ... confirm ...

confirm_deletion() {
    if [[ "$FORCE_YES" == "true" ]]; then return 0; fi
    read -r -p "Delete these branches? (y/N) " response
    [[ "$response" =~ ^[yY]$ ]]
}

# ... delete loop with safety ...
git branch -D -- "$branch"
```

## Acceptance Criteria

- [ ] Script exists at `scripts/cleanup_branches.sh`.
- [ ] Script is executable.
- [ ] Fails gracefully if `jq` is missing.
- [ ] Correctly identifies candidates from audit output.
- [ ] **Interactive Mode**: Does NOT delete without explicit user confirmation.
- [ ] **Agent Mode**: Deletes without confirmation if `--yes` is passed.
- [ ] **Safety**: Never deletes `main`, `master`, `dev`, or current branch.
- [ ] **Output**: Progress logs go to stderr; deleted branches printed to stdout.

## References

- `scripts/audit_branches.sh` (Source of Truth)
- `docs/solutions/git-patterns/detecting-squash-merges.md` (Underlying Logic)
