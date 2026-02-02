---
title: Standardize Repository Maintenance Workflows
type: chore
date: 2026-02-02
---

# Standardize Repository Maintenance Workflows

## Overview

Establish standard workflows for repository maintenance, specifically focusing on **Git Identity** and **Branch Hygiene**.

## Problem Statement

*   **Identity**: Developers occasionally commit with default/generic identities (e.g., "Your Name"), polluting the git history.
*   **Branches**: Local feature branches accumulate after merging, creating clutter and confusion.
*   **Enforcement**: While tools exist (`scripts/check_identity.sh`, `scripts/manage_branches.sh`), they are not enforced or well-documented.

## Proposed Solution

1.  **Enforce Identity**: Install `pre-commit` hooks to block commits with generic authors automatically.
2.  **Document Workflows**: Update `README.md` and `CLAUDE.md` with standard maintenance commands.
3.  **Verify**: Ensure all existing tools are functional and integrated.

## Implementation Steps

### Phase 1: Enforcement
- [ ] Run `pre-commit install` to activate the identity check hook.
- [ ] Verify the hook catches a bad config (dry run).

### Phase 2: Documentation
- [ ] Update `CLAUDE.md` with a `maintenance` or `clean` command alias.
- [ ] Update `README.md` "Utility Scripts" section to include `check_identity.sh` and pre-commit instructions.

### Phase 3: Verification
- [ ] Run `scripts/manage_branches.sh` to ensure no stale branches remain.
- [ ] Run `scripts/audit_commit_authors.sh` to ensure history remains clean.

## Acceptance Criteria
- [ ] `.git/hooks/pre-commit` exists.
- [ ] `git commit` fails if `user.name` is "Your Name".
- [ ] `README.md` documents how to set up these hooks.

## References
- `scripts/check_identity.sh`
- `scripts/manage_branches.sh`
- `.pre-commit-config.yaml`
