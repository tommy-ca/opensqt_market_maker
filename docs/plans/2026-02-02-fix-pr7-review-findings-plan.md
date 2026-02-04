---
title: Fix PR #7 Findings
type: fix
date: 2026-02-02
---

# Fix PR #7 Findings

## Overview

Address critical findings from the code review of PR #7 (Maintenance Workflows).

## Problem Statement

Reviewers identified three key issues:
1.  **Security**: `scripts/check_identity.sh` has a logic bypass (checks config instead of env vars) and uses unsafe `echo`.
2.  **Simplicity**: The same script is overly verbose (checking if variables are set is redundant with Git's own checks).
3.  **Agent-Native**: `clean-branches` in `CLAUDE.md` is interactive by default, creating a trap for agents.

## Proposed Solution

1.  **Refactor `scripts/check_identity.sh`**:
    *   Use `git var GIT_AUTHOR_IDENT` to get the effective identity (security fix).
    *   Use `printf` instead of `echo` (security fix).
    *   Fail fast and remove "is set" checks (simplicity fix).
2.  **Update `CLAUDE.md`**:
    *   Add `force-clean-branches` alias for non-interactive usage.

## Technical Approach

### Refactored `scripts/check_identity.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

# check_identity.sh
# Verifies that the EFFECTIVE git author/email are not generic defaults.

# Get effective identity (respects env vars and config)
IDENT_STRING=$(git var GIT_AUTHOR_IDENT)
# Extract Name (everything before the last <)
AUTHOR_NAME=$(echo "$IDENT_STRING" | sed -r 's/ <.*//')
# Extract Email (between < and >)
AUTHOR_EMAIL=$(echo "$IDENT_STRING" | sed -r 's/.*<(.*)> .*/\1/')

PATTERNS="Your Name|you@example.com|root@localhost|ubuntu@ip-"

FAILED=false

# Use printf to avoid echo flag injection
if printf "%s" "$AUTHOR_NAME" | grep -qEi "$PATTERNS"; then
    echo "❌ Error: Author name '$AUTHOR_NAME' is a generic default."
    FAILED=true
fi

if printf "%s" "$AUTHOR_EMAIL" | grep -qEi "$PATTERNS"; then
    echo "❌ Error: Author email '$AUTHOR_EMAIL' is a generic default."
    FAILED=true
fi

if [[ "$FAILED" = true ]]; then
    exit 1
fi

exit 0
```

### Updated `CLAUDE.md`

```markdown
- clean-branches: scripts/manage_branches.sh --delete         # Interactive branch cleanup
- force-clean-branches: scripts/manage_branches.sh --delete --force # Silent cleanup (Agent Safe)
```

## Acceptance Criteria

- [ ] `check_identity.sh` catches generic names even if set via `GIT_AUTHOR_NAME`.
- [ ] `check_identity.sh` is significantly shorter.
- [ ] `CLAUDE.md` contains the agent-safe cleanup command.

## References

- `docs/plans/2026-02-02-maintenance-and-audit-plan.md`
