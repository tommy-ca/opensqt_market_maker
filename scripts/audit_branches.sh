#!/usr/bin/env bash
set -euo pipefail

# audit_branches.sh
# Audits local branches for merge status (Standard & Squash).
# Outputs JSON or Human-readable list.

JSON_MODE=false

# Argument parsing
while [[ "$#" -gt 0 ]]; do
    case $1 in
        --json) JSON_MODE=true ;;
        *) echo "Unknown parameter: $1"; exit 1 ;;
    esac
    shift
done

# 1. Validation: Safe Context Check
if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "Error: Not a git repository." >&2
    exit 1
fi

# 2. Target Resolution: Check existence to avoid injection/logic errors
TARGET="main"
if ! git show-ref --verify --quiet "refs/heads/$TARGET"; then
    if git show-ref --verify --quiet "refs/heads/master"; then
        TARGET="master"
    else
        echo "Error: Could not determine default branch (main/master)." >&2
        exit 1
    fi
fi

# Initialize JSON output if needed
if [ "$JSON_MODE" = true ]; then
    echo "["
    FIRST_ITEM=true
else
    printf "%-30s | %-12s | %-10s | %s\n" "Branch" "Status" "Date" "Author"
    echo "----------------------------------------------------------------------------------"
fi

# 3. Iteration: Use for-each-ref for safe parsing
# format='%(refname:short)|%(objectname)|%(committerdate:unix)|%(authorname)'
git for-each-ref --format='%(refname:short)|%(objectname)|%(committerdate:unix)|%(authorname)' refs/heads/ | \
while IFS='|' read -r branch sha date author; do
    
    # Skip target branch safely
    if [ "$branch" = "$TARGET" ]; then continue; fi

    # Skip specific protected branches
    case "$branch" in
        "dev"|"develop"|"staging") continue ;;
    esac

    # SECURITY: Use -- to separate branch names from flags
    # Tier 1: Ancestor Check (Fast)
    if git merge-base --is-ancestor -- "$branch" "$TARGET"; then
        status="merged"
    else
        # Tier 2: Tree Hash Check (Squash Detection)
        # Does the content at branch tip exist exactly in target?
        tree_hash=$(git rev-parse "${branch}^{tree}")
        
        # Optimization: Check last 100 commits of target for tree match
        # This covers recent squash merges efficiently
        if git log -n 100 --format='%T' "$TARGET" | grep -q "$tree_hash"; then
            status="squashed"
        else
            status="active"
        fi
    fi

    # Output Logic
    human_date=$(date -d "@$date" +%Y-%m-%d)
    
    if [ "$JSON_MODE" = true ]; then
        if [ "$FIRST_ITEM" = true ]; then
            FIRST_ITEM=false
        else
            echo ","
        fi
        
        # Simple JSON construction (safe for basic strings, watch out for quotes in author)
        # Escaping quotes in author name for JSON safety
        safe_author=$(echo "$author" | sed 's/"/\\"/g')
        
        cat <<EOF
  {
    "branch": "$branch",
    "status": "$status",
    "last_commit_date": "$human_date",
    "timestamp": $date,
    "author": "$safe_author"
  }
EOF
    else
        # Color coding for human output
        color=""
        reset="\033[0m"
        case "$status" in
            "merged") color="\033[32m" ;;   # Green
            "squashed") color="\033[36m" ;; # Cyan
            "active") color="\033[33m" ;;   # Yellow
        esac
        
        if [ -t 1 ]; then
            printf "%-30s | ${color}%-12s${reset} | %-10s | %s\n" "$branch" "$status" "$human_date" "$author"
        else
            printf "%-30s | %-12s | %-10s | %s\n" "$branch" "$status" "$human_date" "$author"
        fi
    fi
done

if [ "$JSON_MODE" = true ]; then
    echo "]"
fi
