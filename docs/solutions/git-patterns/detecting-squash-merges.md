---
title: Robust Squash Merge Detection in Git Scripts
category: git-patterns
tags: [git, bash, scripting, merge-detection]
---

# Robust Squash Merge Detection in Git Scripts

## Context
Standard `git branch --merged` fails for squash merges because the commit hashes change during the squash process. `git cherry` is often used as an alternative, but it can be slow and unreliable for multi-commit squashes or complex histories.

## Solution
Use Tree Hash comparison. Every commit in Git points to a tree object that represents the state of the project files. If a branch is squash-merged, the commit hash changes, but the resulting file content (and thus the tree hash) of the squash commit on the main branch will be identical to the tree hash of the branch tip (assuming no merge conflicts were resolved during the squash in a way that changed the final state).

If `git rev-parse branch^{tree}` matches the tree hash of any commit in `main`, the content is identical and thus the branch can be considered merged (squashed).

## Implementation

The following bash snippet demonstrates this technique, as implemented in `scripts/audit_branches.sh`. It first checks for a standard merge, and if that fails, it performs the tree hash check against the last 100 commits of the target branch.

```bash
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
```

### Key Components

1.  **`git rev-parse "${branch}^{tree}"`**: Extracts the tree hash of the branch tip.
2.  **`git log -n 100 --format='%T' "$TARGET"`**: Lists the tree hashes of the last 100 commits on the target branch (e.g., `main`).
3.  **`grep -q "$tree_hash"`**: Checks if the branch's tree hash exists in the target's history.

This approach efficiently detects squash merges without relying on commit hashes or expensive patch-id calculations.
