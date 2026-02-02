#!/usr/bin/env bash
set -euo pipefail

# check_identity.sh
# Verifies that git user.name and user.email are not set to default/generic values.
# Adapted from scripts/audit_commit_authors.sh

# Patterns to match (case insensitive grep)
# - Your Name (Default git template)
# - you@example.com (Default git template)
# - root@localhost (System default)
# - ubuntu@ip- (AWS/Cloud init default)
PATTERNS="Your Name|you@example.com|root@localhost|ubuntu@ip-"

AUTHOR_NAME=$(git config user.name || echo "")
AUTHOR_EMAIL=$(git config user.email || echo "")

FAILED=false

if [[ -z "$AUTHOR_NAME" ]]; then
    echo "❌ Error: git user.name is not set."
    FAILED=true
fi

if [[ -z "$AUTHOR_EMAIL" ]]; then
    echo "❌ Error: git user.email is not set."
    FAILED=true
fi

if [[ "$FAILED" = true ]]; then
    exit 1
fi

if echo "$AUTHOR_NAME" | grep -qEi "$PATTERNS"; then
    echo "❌ Error: git user.name '$AUTHOR_NAME' is a generic default."
    echo "   Please set it using: git config user.name 'Your Real Name'"
    FAILED=true
fi

if echo "$AUTHOR_EMAIL" | grep -qEi "$PATTERNS"; then
    echo "❌ Error: git user.email '$AUTHOR_EMAIL' is a generic default."
    echo "   Please set it using: git config user.email 'you@yourdomain.com'"
    FAILED=true
fi

if [[ "$FAILED" = true ]]; then
    exit 1
fi

exit 0
