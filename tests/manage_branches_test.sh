#!/usr/bin/env bash
set -e

# manage_branches_test.sh
# Test harness for scripts/manage_branches.sh

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

# Setup temp directory for test repo
TEST_DIR=$(mktemp -d)
SCRIPT_DIR=$(pwd)/scripts
MANAGE_SCRIPT="$SCRIPT_DIR/manage_branches.sh"

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

OUTPUT=$("$MANAGE_SCRIPT" --json)
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

OUTPUT=$("$MANAGE_SCRIPT" --json)
if echo "$OUTPUT" | grep -q '"branch": "feat/merge-commit"' && echo "$OUTPUT" | grep -q '"status": "merged"'; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    echo "Output: $OUTPUT"
    exit 1
fi

# --- Test Case 3: Squash Merge (Exact Tree Match) ---
echo "Running Test Case 3: Squash Merge (Exact Tree Match - Multi Commit)..."
git checkout -b feat/squash-exact
echo "ss1" > ss1.txt
git add ss1.txt
git commit -m "feat: add ss1"
echo "ss2" > ss2.txt
git add ss2.txt
git commit -m "feat: add ss2"
git checkout main
git merge --squash feat/squash-exact
git commit -m "feat: squash ss multi"

OUTPUT=$("$MANAGE_SCRIPT" --json)
# Tier 2 (Cherry) should fail because 1 squash commit != 2 source commits via patch-id
# Tier 3 (Tree Hash) should pass
if echo "$OUTPUT" | grep -q '"branch": "feat/squash-exact"' && echo "$OUTPUT" | grep -q '"status": "squashed-exact"'; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    echo "Output: $OUTPUT"
    exit 1
fi

# --- Test Case 4: Squash Merge (Diff Subset) ---
# This simulates a scenario where the exact tree never existed on main (e.g. messy merge)
# but the content is fully contained.
echo "Running Test Case 4: Squash Merge (Diff Subset)..."
git checkout -b feat/squash-subset
echo "subset" > subset.txt
git add subset.txt
git commit -m "feat: add subset"
git checkout main
# Manually create a commit on main that includes subset.txt AND extra.txt
echo "subset" > subset.txt
echo "extra" > extra.txt
git add subset.txt extra.txt
git commit -m "feat: squash subset plus extra"

OUTPUT=$("$MANAGE_SCRIPT" --json)
# Tier 2 (Cherry) fails (diffs different)
# Tier 3 (Tree Hash) fails (tree never existed)
# Tier 4 (Diff Subset) passes
if echo "$OUTPUT" | grep -q '"branch": "feat/squash-subset"' && echo "$OUTPUT" | grep -q '"status": "squashed-subset"'; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    echo "Output: $OUTPUT"
    exit 1
fi

# --- Test Case 5: Cherry Picked ---
echo "Running Test Case 5: Cherry Picked..."
git checkout -b feat/cherry
echo "cherry" > cherry.txt
git add cherry.txt
git commit -m "feat: add cherry"
git checkout main
# Diverge main so cherry-pick results in different hash
echo "diverge-cherry" > diverge-cherry.txt
git add diverge-cherry.txt
git commit -m "chore: diverge main for cherry"
git cherry-pick feat/cherry

OUTPUT=$("$MANAGE_SCRIPT" --json)
if echo "$OUTPUT" | grep -q '"branch": "feat/cherry"' && echo "$OUTPUT" | grep -q '"status": "cherry-picked"'; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    echo "Output: $OUTPUT"
    exit 1
fi

# --- Test Case 6: Active Branch ---
echo "Running Test Case 6: Active Branch..."
git checkout -b feat/active
echo "active" > active.txt
git add active.txt
git commit -m "feat: active work"
git checkout main

OUTPUT=$("$MANAGE_SCRIPT" --json)
if echo "$OUTPUT" | grep -q '"branch": "feat/active"' && echo "$OUTPUT" | grep -q '"status": "active"'; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    echo "Output: $OUTPUT"
    exit 1
fi

# --- Test Case 7: Protected Branch ---
echo "Running Test Case 7: Protected Branch..."
git checkout -b release/v1.0
echo "release" > release.txt
git add release.txt
git commit -m "feat: release branch"
git checkout main

OUTPUT=$("$MANAGE_SCRIPT" --json)
if echo "$OUTPUT" | grep -q '"branch": "release/v1.0"' && echo "$OUTPUT" | grep -q '"status": "protected"'; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    echo "Output: $OUTPUT"
    exit 1
fi

# --- Test Case 8: Cleanup (Delete) ---
echo "Running Test Case 8: Cleanup (Delete)..."
# Should delete: feat/ff-merge, feat/merge-commit, feat/squash-exact, feat/squash-subset, feat/cherry
# Should NOT delete: feat/active, release/v1.0

# Using --force --delete
"$MANAGE_SCRIPT" --delete --force

# Verify deletions
REMAINING=$(git branch)
if echo "$REMAINING" | grep -q "feat/ff-merge"; then echo -e "${RED}FAIL: feat/ff-merge not deleted${NC}"; exit 1; fi
if echo "$REMAINING" | grep -q "feat/active"; then echo -e "${GREEN}PASS: feat/active preserved${NC}"; else echo -e "${RED}FAIL: feat/active deleted${NC}"; exit 1; fi
if echo "$REMAINING" | grep -q "release/v1.0"; then echo -e "${GREEN}PASS: release/v1.0 preserved${NC}"; else echo -e "${RED}FAIL: release/v1.0 deleted${NC}"; exit 1; fi

echo -e "${GREEN}All tests passed!${NC}"

# Cleanup
cd ..
rm -rf "$TEST_DIR"
