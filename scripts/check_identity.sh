#!/usr/bin/env bash
set -euo pipefail

# check_identity.sh
# Verifies that the EFFECTIVE git author identity is not a generic default.

# Get effective identity (respects env vars and config)
IDENT=$(git var GIT_AUTHOR_IDENT)
PATTERNS="Your Name|you@example.com|root@localhost|ubuntu@ip-"

# Check full identity string once for simplicity and robustness
if printf "%s" "$IDENT" | grep -qEi "$PATTERNS"; then
    echo "❌ Error: Git identity contains generic default values:"
    echo "   $IDENT"
    echo "   Please set your name and email correctly."
    exit 1
fi

exit 0
