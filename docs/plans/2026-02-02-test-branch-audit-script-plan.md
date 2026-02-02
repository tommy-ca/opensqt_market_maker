# Test Plan: Branch Audit Script

**Date:** 2026-02-02
**Author:** Antigravity

## Overview
This document outlines the test plan for the branch audit bash script. The goal is to ensure the script accurately identifies merged, squash-merged, and active branches, and outputs valid JSON for downstream processing.

## 1. Unit Testing Strategy

We will use **BATS (Bash Automated Testing System)** or a lightweight custom test runner to verify the script's logic. The tests will simulate a git repository environment, creating branches and commits to represent various states, and then asserting the script's output against expected results.

### Framework Choice
- **Primary:** `bats-core` (if available in the environment)
- **Fallback:** A custom shell script that sets up a temporary git repo, runs scenarios, and checks exit codes/output.

## 2. Test Cases

The following scenarios must be covered to ensure robust detection of branch states.

### 2.1. Standard Merge (Fast-Forward)
- **Setup:**
  - Create `feature/ff-merge`.
  - Add a commit.
  - Switch to `main`.
  - Merge `feature/ff-merge` (fast-forward).
- **Expectation:**
  - Status: `merged`
  - Reason: `merged`

### 2.2. Standard Merge (Merge Commit)
- **Setup:**
  - Create `feature/merge-commit`.
  - Add a commit.
  - Switch to `main`.
  - Add a distinct commit on `main` to force a divergence.
  - Merge `feature/merge-commit` (creating a merge commit).
- **Expectation:**
  - Status: `merged`
  - Reason: `merged`

### 2.3. Squash Merge (Single Commit)
- **Setup:**
  - Create `feature/squash-single`.
  - Add one commit.
  - Switch to `main`.
  - Squash merge `feature/squash-single` (`git merge --squash`).
  - Commit the squash.
- **Expectation:**
  - Status: `merged`
  - Reason: `squash_merged` (detected via `git cherry` equivalent logic)

### 2.4. Squash Merge (Multi-Commit Source)
- **Setup:**
  - Create `feature/squash-multi`.
  - Add multiple commits.
  - Switch to `main`.
  - Squash merge `feature/squash-multi`.
  - Commit the squash.
- **Expectation:**
  - Status: `merged`
  - Reason: `squash_merged`

### 2.5. Active / Diverged Branch
- **Setup:**
  - Create `feature/active`.
  - Add commits that are *not* on `main`.
- **Expectation:**
  - Status: `active`
  - Reason: `unmerged` or `diverged`

### 2.6. Protected Branches
- **Setup:**
  - Ensure `main`, `master`, `dev`, `development` branches exist.
- **Expectation:**
  - These branches should be excluded from the report or marked specifically as protected/skipped, depending on script logic.
  - They should never be identified as candidates for deletion.

### 2.7. JSON Output Validation
- **Setup:**
  - Run the script against a repository with a mix of the above states.
- **Validation:**
  - Pipe output to `jq .`.
  - Verify structure:
    ```json
    [
      {
        "branch": "feature/xyz",
        "status": "merged|active",
        "reason": "..."
      },
      ...
    ]
    ```
  - Ensure no invalid characters or broken JSON syntax.

## 3. CI Integration Proposal

To ensure the audit script remains reliable, we will integrate these tests into the project's CI pipeline.

- **Trigger:**
  - Push to `main` affecting `scripts/`.
  - Pull Request targeting `main` affecting `scripts/`.
- **Job Steps:**
  1. Checkout code.
  2. Install BATS (if using).
  3. Run `tests/audit_script_test.sh`.
  4. Fail the build if any test case fails.

---
**Next Steps:**
1. Create the test harness (`tests/audit_script_test.sh`).
2. Implement the test cases defined in Section 2.
3. Verify the audit script against the harness.
