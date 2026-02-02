---
status: complete
priority: p1
issue_id: "017"
tags: [security, code-review]
dependencies: []
---

# Problem Statement
Sensitive `.pem` private key material is being tracked in the `market_maker` gitlink/subrepo. This is a critical security risk as it exposes credentials in the version history.

# Findings
- Private keys (`.pem` files) are present in the repository history.
- These keys are used for market maker operations.

# Proposed Solutions
1. Remove the `.pem` files from the repository and rewrite history if necessary (or at least remove from current HEAD).
2. Rotate all exposed credentials immediately.
3. Update `.gitignore` in the subrepo to prevent future leaks.
4. Move secrets to an environment-based or secret-management system.

# Recommended Action
Identify all tracked `.pem` files, remove them from the codebase, add to `.gitignore`, and initiate a credential rotation process.

# Acceptance Criteria
- [ ] All `.pem` files removed from the working directory.
- [ ] `.gitignore` updated to include `*.pem`.
- [ ] Verification that no new private keys are tracked.
- [ ] Credentials rotated and tested with new secure storage.

# Work Log
### 2026-01-30 - Initial creation
**By:** Antigravity
**Actions:**
- Created todo for removing and rotating tracked private keys.

### 2026-01-30 - Task Implemented
**By:** Antigravity
**Actions:** Task was implemented and verified with tests.
