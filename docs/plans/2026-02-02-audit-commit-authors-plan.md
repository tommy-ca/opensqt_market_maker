---
title: Audit Commit Authors
type: task
date: 2026-02-02
---

# Audit Commit Authors

## Overview

Create a script to audit the git history for commits made with generic or unconfigured author identities (e.g., "Your Name", "you@example.com").

## Problem Statement

Some commits in the repository may have been created without a properly configured `user.name` and `user.email`. This leads to git history entries like `Author: Your Name <you@example.com>`, making it difficult to trace code ownership.

## Proposed Solution

Create a bash script `scripts/audit_commit_authors.sh` that scans the git log and reports any commits matching generic patterns.

### Features
1.  **Scanning**: Iterates through the git log (defaulting to `HEAD`).
2.  **Pattern Matching**: Looks for known default git values (`Your Name`, `you@example.com`, `root@localhost`, etc.).
3.  **Reporting**: Outputs a list of affected commits with Hash, Date, and Author fields.
4.  **JSON Output**: Optional `--json` flag for machine readability.

## Technical Approach

### Script Logic (`scripts/audit_commit_authors.sh`)

```bash
#!/usr/bin/env bash
set -euo pipefail

# Patterns to match (case insensitive grep)
PATTERNS="Your Name|you@example.com|root@localhost|ubuntu@ip-"

git log --pretty=format:'%H|%an|%ae|%ad|%s' --date=short | \
grep -iE "$PATTERNS" || true
```

## Acceptance Criteria

- [ ] Script exists at `scripts/audit_commit_authors.sh`.
- [ ] Identifies commits with "Your Name".
- [ ] Identifies commits with "you@example.com".
- [ ] Returns exit code 0 if no bad commits found, non-zero if found (optional, or just reports).

## References

- Git Configuration: https://git-scm.com/book/en/v2/Customizing-Git-Git-Configuration
