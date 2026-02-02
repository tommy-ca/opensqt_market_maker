#!/usr/bin/env bash
set -euo pipefail

# audit_commit_authors.sh
# Scans git log for commits with generic or unconfigured author identities.

JSON_MODE=false

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --json) JSON_MODE=true ;;
        *) echo "Unknown parameter: $1"; exit 1 ;;
    esac
    shift
done

# Patterns to match (case insensitive grep)
# - Your Name (Default git template)
# - you@example.com (Default git template)
# - root@localhost (System default)
# - ubuntu@ip- (AWS/Cloud init default)
PATTERNS="Your Name|you@example.com|root@localhost|ubuntu@ip-"

# Get bad commits
# Format: Hash|AuthorName|AuthorEmail|Date|Subject
BAD_COMMITS=$(git log --all --pretty=format:'%H|%an|%ae|%ad|%s' --date=short | grep -iE "$PATTERNS" || true)

if [ -z "$BAD_COMMITS" ]; then
    if [ "$JSON_MODE" = true ]; then
        echo "[]"
    else
        echo "✅ No generic commit authors found."
    fi
    exit 0
fi

if [ "$JSON_MODE" = true ]; then
    echo "["
    FIRST=true
    # Process line by line
    echo "$BAD_COMMITS" | while IFS='|' read -r hash name email date subject; do
        if [ "$FIRST" = true ]; then
            FIRST=false
        else
            echo ","
        fi
        # Escape quotes for JSON
        safe_subject=$(echo "$subject" | sed 's/"/\\"/g')
        cat <<EOF
  {
    "hash": "$hash",
    "author_name": "$name",
    "author_email": "$email",
    "date": "$date",
    "subject": "$safe_subject"
  }
EOF
    done
    echo "]"
else
    echo "⚠️  Found commits with generic authors:"
    echo "--------------------------------------------------------------------------------"
    printf "%-10s | %-20s | %-25s | %-10s | %s\n" "Hash" "Name" "Email" "Date" "Subject"
    echo "--------------------------------------------------------------------------------"
    echo "$BAD_COMMITS" | while IFS='|' read -r hash name email date subject; do
        short_hash=${hash:0:8}
        short_subj=${subject:0:40}
        printf "%-10s | %-20s | %-25s | %-10s | %s\n" "$short_hash" "$name" "$email" "$date" "$short_subj"
    done
    echo "--------------------------------------------------------------------------------"
    exit 1
fi
