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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AUDIT_SCRIPT="$SCRIPT_DIR/audit_branches.sh"

# 1. Dependency Checks
if ! command -v jq >/dev/null 2>&1; then
    echo "Error: 'jq' is required but not installed." >&2
    exit 1
fi

if [[ ! -x "$AUDIT_SCRIPT" ]]; then
    echo "Error: audit_branches.sh not found or not executable at $AUDIT_SCRIPT" >&2
    exit 1
fi

# 2. Run Audit
echo "Scanning branches..."
AUDIT_JSON=$("$AUDIT_SCRIPT" --json)

# 3. Filter Candidates
# Select branches that are merged OR squashed
# Exclude the current branch just in case (audit script handles this, but belt & suspenders)
CURRENT_BRANCH=$(git branch --show-current)

CANDIDATES=$(echo "$AUDIT_JSON" | jq -r --arg current "$CURRENT_BRANCH" '
    .[] | 
    select(.status == "merged" or .status == "squashed") |
    select(.branch != $current) |
    "[\(.status)] \(.branch)"
')

if [[ -z "$CANDIDATES" ]]; then
    echo "All clean! No merged or squashed branches found."
    exit 0
fi

# 4. Display Candidates
COUNT=$(echo "$CANDIDATES" | wc -l)
echo "Found $COUNT branch(es) safe to delete:"
echo "----------------------------------------"
echo "$CANDIDATES"
echo "----------------------------------------"

# 5. Interactive Confirmation
confirm_deletion() {
    if [[ "$FORCE_YES" == "true" ]]; then return 0; fi
    read -r -p "Delete these $COUNT branches? (y/N) " response
    [[ "$response" =~ ^[yY]$ ]]
}

if ! confirm_deletion; then
    echo "Aborted."
    exit 0
fi

# 6. Execute Deletion
# We re-parse the JSON to get raw branch names for safety (avoiding parsing the display string)
BRANCHES_TO_DELETE=$(echo "$AUDIT_JSON" | jq -r --arg current "$CURRENT_BRANCH" '
    .[] | 
    select(.status == "merged" or .status == "squashed") |
    select(.branch != $current) |
    .branch
')

echo "$BRANCHES_TO_DELETE" | while read -r branch; do
    if [[ -n "$branch" ]]; then
        # Force delete (-D) because squash-merged branches often fail the -d check
        if git branch -D "$branch"; then
             echo "Deleted $branch"
        else
             echo "Failed to delete $branch" >&2
        fi
    fi
done

echo "Cleanup complete."
