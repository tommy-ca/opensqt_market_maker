#!/usr/bin/env bash
set -e

# test_audit_branches.sh
# Test harness for scripts/audit_branches.sh

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

# Setup temp directory for test repo
TEST_DIR=$(mktemp -d)
SCRIPT_DIR=$(pwd)/scripts
AUDIT_SCRIPT="$SCRIPT_DIR/audit_branches.sh"

echo "Setting up test repo in $TEST_DIR..."
cd "$TEST_DIR"
git init -b main
git config user.email "test@example.com"
git config user.name "Test User"

# Initial commit
echo "init" > README.md
git add README.md
git commit -m "Initial commit"

# --- Test Case 1: Standard Merge (Fast-Forward) ---
echo "Running Test Case 1: Standard Merge (Fast-Forward)..."
git checkout -b feat/ff-merge
echo "ff" > ff.txt
git add ff.txt
git commit -m "feat: add ff"
git checkout main
git merge feat/ff-merge

OUTPUT=$("$AUDIT_SCRIPT" --json)
if echo "$OUTPUT" | grep -q '"branch": "feat/ff-merge"' && echo "$OUTPUT" | grep -q '"status": "merged"'; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    echo "Output: $OUTPUT"
    exit 1
fi

# --- Test Case 2: Standard Merge (Merge Commit) ---
echo "Running Test Case 2: Standard Merge (Merge Commit)..."
git checkout -b feat/merge-commit
echo "mc" > mc.txt
git add mc.txt
git commit -m "feat: add mc"
git checkout main
echo "diverge" > diverge.txt
git add diverge.txt
git commit -m "chore: diverge main"
git merge --no-edit feat/merge-commit

OUTPUT=$("$AUDIT_SCRIPT" --json)
if echo "$OUTPUT" | grep -q '"branch": "feat/merge-commit"' && echo "$OUTPUT" | grep -q '"status": "merged"'; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    echo "Output: $OUTPUT"
    exit 1
fi

# --- Test Case 3: Squash Merge (Single Commit) ---
echo "Running Test Case 3: Squash Merge (Single Commit)..."
git checkout -b feat/squash-single
echo "ss" > ss.txt
git add ss.txt
git commit -m "feat: add ss"
git checkout main
git merge --squash feat/squash-single
git commit -m "feat: squash ss"

OUTPUT=$("$AUDIT_SCRIPT" --json)
if echo "$OUTPUT" | grep -q '"branch": "feat/squash-single"' && echo "$OUTPUT" | grep -q '"status": "squashed"'; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    echo "Output: $OUTPUT"
    exit 1
fi

# --- Test Case 4: Active Branch ---
echo "Running Test Case 4: Active Branch..."
git checkout -b feat/active
echo "active" > active.txt
git add active.txt
git commit -m "feat: active work"
git checkout main

OUTPUT=$("$AUDIT_SCRIPT" --json)
if echo "$OUTPUT" | grep -q '"branch": "feat/active"' && echo "$OUTPUT" | grep -q '"status": "active"'; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    echo "Output: $OUTPUT"
    exit 1
fi

# Cleanup
cd ..
rm -rf "$TEST_DIR"
echo -e "${GREEN}All tests passed!${NC}"
