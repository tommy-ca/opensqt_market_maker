---
status: resolved
priority: p1
issue_id: 014
tags: [code-review, pattern-recognition, bug, type-error]
dependencies: []
---

# BaseAdapter Type Mismatch Bug in SetParseError Method

## Problem Statement

**Location**: `internal/exchange/base/adapter.go:63`

Type mismatch bug where method receiver is wrong type:

```go
// WRONG - receiver is ParseError instead of BaseAdapter
func (b *ParseError) SetParseError(fn ParseErrorFunc) {
    b.parseError = fn
}
```

This code compiles but is semantically incorrect - `SetParseError` should be a method on `BaseAdapter`, not on the error type itself.

**Impact**:
- Cannot set custom parse error handlers on adapter instances
- Breaks intended configuration pattern
- May cause runtime panics if called

## Proposed Solution

**Fix** (1 hour):
```go
// CORRECT - receiver is BaseAdapter
func (b *BaseAdapter) SetParseError(fn ParseErrorFunc) {
    b.parseError = fn
}
```

## Acceptance Criteria

- [ ] Method receiver changed to `*BaseAdapter`
- [ ] All tests pass
- [ ] Can successfully set custom parse error handlers

## Resources

- Pattern Recognition Report: See agent findings
- File: `internal/exchange/base/adapter.go`
