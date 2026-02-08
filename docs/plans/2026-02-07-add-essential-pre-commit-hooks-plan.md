---
title: Add Essential Pre-Commit Hooks
type: feat
date: 2026-02-07
---

# Add Essential Pre-Commit Hooks

## Overview

Enhance the `.pre-commit-config.yaml` to include essential checks for security, documentation, and script quality. This fills the gaps identified in the current configuration and aligns with industry best practices for a multi-language repo (Go, Python, Proto, Shell).

## Problem Statement

The current pre-commit configuration covers basic whitespace, Go linting, and Python linting. However, it lacks:
1.  **Security**: No secrets detection (risk of leaking API keys).
2.  **Shell Quality**: No `shellcheck` for the `scripts/` directory.
3.  **Documentation Standards**: No markdown formatting (prettier).
4.  **Proto Standards**: No proto linting despite `buf.yaml` existing.

## Proposed Solution

Add the following hooks:
1.  **gitleaks** (via `zricethezav/gitleaks`): Faster and more robust than `detect-secrets` for secret scanning.
2.  **shellcheck** (via `koalaman/shellcheck-pre-commit`): Validate shell scripts.
3.  **prettier** (via `pre-commit/mirrors-prettier`): Standardize Markdown, YAML, and JSON.
4.  **buf-lint** (via `bufbuild/buf`): Enforce protobuf standards.

## Implementation Plan

### 1. Specification & Test
Create `docs/specs/pre_commit_standards.md` defining the required hooks and their configurations.

### 2. Configuration Update
Update `.pre-commit-config.yaml` to include the new repositories.

### 3. Verification
1.  **Secrets**: Create a test file with a fake secret (locally) and verify `gitleaks` blocks it.
2.  **Shell**: Create a test script with a syntax error and verify `shellcheck` catches it.
3.  **Run All**: Run against all files to ensure no existing code breaks the build (fix or exclude if necessary).

## Acceptance Criteria
- [ ] `.pre-commit-config.yaml` contains `gitleaks`, `shellcheck`, `prettier`, and `buf-lint` (or equivalent).
- [ ] `pre-commit run --all-files` passes on the current codebase.
- [ ] Attempting to commit a fake secret fails.

## References
- [Pre-commit Hooks](https://pre-commit.com/hooks.html)
- [Gitleaks](https://github.com/zricethezav/gitleaks)
