#!/usr/bin/env bash
set -euo pipefail

# manage_branches.sh
# Combined tool for auditing and cleaning up local branches.
# Features:
# - Robust parsing of git refs (handles special chars)
# - Multi-tier merge detection (Ancestor, Cherry, Tree-Hash, Diff-Subset)
# - Interactive or Forced cleanup
# - JSON output for tooling

# --- Configuration ---
PROTECTED_REGEX="^(main|master|dev|develop|staging|release/.*)$"
TARGET_DEPTH=1000

# --- State ---
JSON_MODE=false
DELETE_MODE=false
FORCE_MODE=false

# --- Argument Parsing ---
while [[ "$#" -gt 0 ]]; do
    case $1 in
        --json) JSON_MODE=true ;;
        --delete) DELETE_MODE=true ;;
        --force) FORCE_MODE=true ;;
        -y|--yes) FORCE_MODE=true ;; # Alias for force
        *) echo "Unknown parameter: $1"; exit 1 ;;
    esac
    shift
done

# --- Helpers ---
log() {
    # If in JSON mode, log to stderr to avoid corrupting stdout
    echo "$@" >&2
}

# Escape for JSON string
json_escape() {
    local s="$1"
    s="${s//\\/\\\\}"
    s="${s//\"/\\\"}"
    s="${s//$'\n'/\\n}"
    s="${s//$'\r'/\\r}"
    s="${s//$'\t'/\\t}"
    echo "$s"
}

# --- 1. Validation & Setup ---
if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    log "Error: Not a git repository."
    exit 1
fi

CURRENT_BRANCH=$(git branch --show-current)

# Target Resolution (main or master)
TARGET="main"
if ! git show-ref --verify --quiet "refs/heads/$TARGET"; then
    if git show-ref --verify --quiet "refs/heads/master"; then
        TARGET="master"
    else
        log "Error: Could not determine default branch (main/master)."
        exit 1
    fi
fi

# Pre-fetch target tree hashes for performance (Squash Tier 3)
# We fetch the last N commits' tree hashes into a single string for grep
TARGET_TREE_HISTORY=$(git log -n "$TARGET_DEPTH" --format='%T' "$TARGET")

# --- 2. Audit Phase ---
# We accumulate results in arrays to allow for JSON output or Interactive Cleanup
declare -a BRANCHES
declare -a STATUSES
declare -a DATES
declare -a AUTHORS

# Safe delimiter: 0x1F (Unit Separator)
SEP=$(printf "\x1F")

# Iterate all local heads
# Format: refname:short | objectname | committerdate:unix | authorname
while IFS="$SEP" read -r branch sha date author; do
    # Skip target branch
    if [ "$branch" = "$TARGET" ]; then continue; fi

    status="active"

    # Protected Branch Check
    if [[ "$branch" =~ $PROTECTED_REGEX ]]; then
        status="protected"
    
    # Tier 1: Ancestor Check (Standard Merge / Fast-Forward)
    elif git merge-base --is-ancestor -- "$branch" "$TARGET"; then
        status="merged"
    
    else
        # Tier 2: Cherry Check (Rebase/Patch-ID detection)
        # If git cherry returns no lines starting with "+", all commits are in target
        if [ -z "$(git cherry "$TARGET" "$branch" | grep '^+' || true)" ]; then
            status="cherry-picked"
        
        # Tier 3: Tree Hash Check (Exact Squash Detection)
        # Does the content at branch tip exist exactly in target history?
        elif echo "$TARGET_TREE_HISTORY" | grep -q "$(git rev-parse "${branch}^{tree}")"; then
            status="squashed-exact"
            
        # Tier 4: Subset Check (Diverged Squash Detection)
        # If 'git diff branch target' shows NO removals (lines starting with -), 
        # then target contains everything in branch (plus potential extras).
        # We ignore lines starting with '---' (file headers) and binary file diffs.
        elif ! git diff "$branch" "$TARGET" | grep -q "^-[^-]"; then
            status="squashed-subset"
        fi
    fi

    BRANCHES+=("$branch")
    STATUSES+=("$status")
    DATES+=("$(date -d "@$date" +%Y-%m-%d)")
    AUTHORS+=("$author")

done < <(git for-each-ref --format="%(refname:short)${SEP}%(objectname)${SEP}%(committerdate:unix)${SEP}%(authorname)" refs/heads/)

# --- 3. Output Phase ---

if [ "$JSON_MODE" = true ]; then
    echo "["
    count=${#BRANCHES[@]}
    for (( i=0; i<count; i++ )); do
        safe_branch=$(json_escape "${BRANCHES[$i]}")
        safe_author=$(json_escape "${AUTHORS[$i]}")
        
        echo "  {"
        echo "    \"branch\": \"$safe_branch\","
        echo "    \"status\": \"${STATUSES[$i]}\","
        echo "    \"last_commit_date\": \"${DATES[$i]}\","
        echo "    \"author\": \"$safe_author\""
        if [ $i -lt $((count-1)) ]; then
            echo "  },"
        else
            echo "  }"
        fi
    done
    echo "]"
elif [ "$DELETE_MODE" = false ]; then
    # Human Readable Table
    printf "%-30s | %-15s | %-10s | %s\n" "Branch" "Status" "Date" "Author"
    echo "---------------------------------------------------------------------------------------"
    
    count=${#BRANCHES[@]}
    for (( i=0; i<count; i++ )); do
        color=""
        reset="\033[0m"
        case "${STATUSES[$i]}" in
            "merged"|"squashed-exact"|"squashed-subset"|"cherry-picked") color="\033[32m" ;; # Green
            "protected") color="\033[36m" ;; # Cyan
            "active") color="\033[33m" ;;    # Yellow
        esac
        
        if [ -t 1 ]; then
            printf "%-30s | ${color}%-15s${reset} | %-10s | %s\n" "${BRANCHES[$i]}" "${STATUSES[$i]}" "${DATES[$i]}" "${AUTHORS[$i]}"
        else
            printf "%-30s | %-15s | %-10s | %s\n" "${BRANCHES[$i]}" "${STATUSES[$i]}" "${DATES[$i]}" "${AUTHORS[$i]}"
        fi
    done
fi

# --- 4. Cleanup Phase ---
if [ "$DELETE_MODE" = true ]; then
    log ""
    log "--- Cleanup Mode ---"
    
    # Filter candidates
    declare -a CANDIDATES
    count=${#BRANCHES[@]}
    for (( i=0; i<count; i++ )); do
        s="${STATUSES[$i]}"
        b="${BRANCHES[$i]}"
        
        # Safety checks
        if [[ "$b" == "$CURRENT_BRANCH" ]]; then continue; fi
        if [[ "$s" == "active" || "$s" == "protected" ]]; then continue; fi
        
        CANDIDATES+=("$b")
    done
    
    cand_count=${#CANDIDATES[@]}
    
    if [ "$cand_count" -eq 0 ]; then
        log "No deletable branches found."
        exit 0
    fi
    
    # Interactive Confirmation
    if [ "$FORCE_MODE" = false ]; then
        log "Candidates for deletion:"
        for b in "${CANDIDATES[@]}"; do
            log "  - $b"
        done
        log ""
        read -r -p "Delete these $cand_count branches? (y/N) " response
        if [[ ! "$response" =~ ^[yY]$ ]]; then
            log "Aborted."
            exit 0
        fi
    fi
    
    # Execute Delete
    for b in "${CANDIDATES[@]}"; do
        # Use -- to prevent flag injection
        if git branch -D -- "$b" >/dev/null 2>&1; then
            log "Deleted $b"
        else
            log "Failed to delete $b"
        fi
    done
fi
