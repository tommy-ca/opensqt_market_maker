---
status: pending
priority: p3
issue_id: "046"
tags: [code-review, security]
dependencies: []
---

# Enforce HTTPS

Security hardening for exchange BaseURLs to ensure production endpoints use HTTPS.

## Problem Statement

Some exchange BaseURLs might not be strictly enforcing HTTPS, which could expose sensitive data or API keys to intercept in transit. Hardening these endpoints to ensure `https://` is mandatory for production is a critical security measure.

## Findings

- Security hardening needed for exchange BaseURLs.

## Proposed Solutions

### Option 1: Validate URLs at runtime

**Approach:** Add validation logic to the exchange clients to error out or automatically upgrade to HTTPS if a production URL is provided with `http://`.

**Pros:**
- Prevents accidental use of insecure protocols.
- Ensures data integrity and confidentiality.

**Cons:**
- May require configuration changes if any legacy internal endpoints still use HTTP.

**Effort:** 1 hour

**Risk:** Low

## Recommended Action

**To be filled during triage.**

## Acceptance Criteria

- [ ] All production exchange BaseURLs use `https://`.
- [ ] Runtime validation added to prevent insecure protocols in production.

## Work Log

### 2026-02-10 - Task Created

**By:** Antigravity

**Actions:**
- Created todo from P3 finding.
