#!/usr/bin/env bash
set -euo pipefail

# check_identity.sh
# Verifies that the EFFECTIVE git author/email are not generic defaults.

# Get effective identity (respects env vars and config)
# git var GIT_AUTHOR_IDENT returns format: "Name <email> timestamp tz"
IDENT_STRING=$(git var GIT_AUTHOR_IDENT)

# Extract Name (everything before the last <)
AUTHOR_NAME=$(echo "$IDENT_STRING" | sed -r 's/ <.*//')
# Extract Email (between < and >)
AUTHOR_EMAIL=$(echo "$IDENT_STRING" | sed -r 's/.*<(.*)> .*/\1/')

# Patterns to match (case insensitive grep)
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
