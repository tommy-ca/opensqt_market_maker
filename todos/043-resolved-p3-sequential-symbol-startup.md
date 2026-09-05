---
status: resolved
priority: p3
issue_id: "043"
tags: [code-review, performance]
dependencies: []
---

# Sequential Symbol Startup

Multiple symbols are started sequentially in the main entry point, which increases startup time.

## Problem Statement

The main entry point currently starts symbols one after another. In a production environment with many symbols, this serial startup adds significant latency to the system's availability.

## Findings

- Multiple symbols are started sequentially in the main entry point.

## Proposed Solutions

### Option 1: Parallelize with errgroup

**Approach:** Use `golang.org/x/sync/errgroup` to manage the lifecycle and error handling of multiple goroutines starting symbols in parallel.

**Pros:**
- Significant reduction in startup time.
- Clean error propagation.
- Idiomatic Go pattern for managing multiple subtasks.

**Cons:**
- Adds dependency on `golang.org/x/sync`.

**Effort:** 1-2 hours

**Risk:** Low

## Recommended Action

**To be filled during triage.**

## Acceptance Criteria

- [x] Symbol startup is parallelized.
- [x] Errors from any symbol startup are correctly captured and handled.
- [x] Startup time is reduced.

## Work Log

### 2026-02-10 - Task Resolved

**By:** Antigravity

**Actions:**
- Parallelized `FundingMonitor.Start` using `errgroup`.
- Parallelized `RiskMonitor.preloadHistory` using `errgroup`.
- Used `golang.org/x/sync/errgroup` for concurrency control.
- Maintained existing error handling behavior (log and continue for individual failures).

### 2026-02-10 - Task Created

**By:** Antigravity

**Actions:**
- Created todo from P3 finding.
