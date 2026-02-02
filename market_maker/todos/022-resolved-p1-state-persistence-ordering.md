---
status: resolved
priority: p1
issue_id: 022
tags: [code-review, data-integrity, critical, race-condition, state-persistence]
dependencies: []
---

# State Persistence Can Fail After In-Memory State Change

## Problem Statement

**Location**: `internal/engine/simple/engine.go:228-232`

State is updated in memory BEFORE being persisted to database. If persistence fails, in-memory state is already changed but database is stale:

```go
// Update in-memory state
if err := e.positionManager.OnOrderUpdate(ctx, update); err != nil {
    return err
}

// Save to database
if err := e.store.SaveState(ctx, newState); err != nil {
    e.logger.Error("Failed to save state", "error", err)
    return err  // ⚠️ State already changed in memory, but not persisted!
}
```

**Impact**:
- **CRITICAL**: In-memory and persisted state diverge
- **Crash recovery failure**: Restart loads stale state from database
- **Position tracking corruption**: Positions reset to old values after restart
- **Financial risk**: Trading resumes with incorrect position assumptions
- **Data integrity violation**: Single source of truth principle broken

## Evidence

From Data Integrity Guardian review:
> "The in-memory position state is updated before the database write. If the database write fails (disk full, network issue, etc.), the in-memory state has already changed but the persisted state has not. On restart, the engine will load stale state."

## Failure Scenario

**Timeline**:
1. **T=0**: Order fills, OnOrderUpdate called
2. **T=1**: Position manager updates in-memory position: 100 → 150 BTC ✓
3. **T=2**: SaveState called to persist
4. **T=3**: Database write fails (disk full, I/O error, SQLite locked)
5. **T=4**: Error logged, function returns error
6. **T=5**: **In-memory**: Position = 150 BTC (updated)
7. **T=5**: **Database**: Position = 100 BTC (stale)
8. **T=10**: System crashes (unrelated reason)
9. **T=11**: Restart loads from database: Position = 100 BTC
10. **T=12**: System thinks position = 100, actual exchange position = 150
11. **T=13**: Places order assuming 100 BTC → Actual position 200 BTC
12. **T=14**: Risk limit exceeded → **Financial loss**

## Root Cause Analysis

**Ordering violation**: State mutation before persistence violates database transaction principles.

**Correct pattern** (ACID):
1. Begin transaction
2. Update database
3. Commit transaction
4. **ONLY THEN** update in-memory cache

**Current pattern** (broken):
1. Update in-memory state
2. **ATTEMPT** to update database
3. If fails, in-memory already changed

## Proposed Solutions

### Option 1: Reverse Order (Persist First) - Recommended

**Effort**: 4-6 hours

```go
func (e *SimpleEngine) processOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
    // 1. Build new state snapshot
    newState, err := e.buildStateSnapshot(ctx)
    if err != nil {
        return fmt.Errorf("failed to build state snapshot: %w", err)
    }

    // 2. Apply update to snapshot (not to live state yet)
    if err := applyUpdateToSnapshot(newState, update); err != nil {
        return fmt.Errorf("failed to apply update to snapshot: %w", err)
    }

    // 3. Persist to database FIRST
    if err := e.store.SaveState(ctx, newState); err != nil {
        e.logger.Error("Failed to save state", "error", err)
        return fmt.Errorf("state persistence failed: %w", err)
        // In-memory state NOT changed yet - safe to return error
    }

    // 4. ONLY AFTER successful persistence, update in-memory state
    if err := e.positionManager.OnOrderUpdate(ctx, update); err != nil {
        // This should never fail if snapshot application succeeded
        // But if it does, we have bigger problems
        e.logger.Error("CRITICAL: In-memory update failed after persistence", "error", err)
        return fmt.Errorf("critical: state desync: %w", err)
    }

    return nil
}
```

**Benefits**:
- Failure before in-memory change → No divergence
- In-memory state is always ≥ persisted state (never behind)
- Crash recovery is safe

**Trade-off**: Two state update operations (snapshot + in-memory)

---

### Option 2: Rollback on Failure

**Effort**: 6-8 hours

```go
func (e *SimpleEngine) processOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
    // 1. Snapshot current state for rollback
    snapshot := e.positionManager.CreateSnapshot()

    // 2. Update in-memory state
    if err := e.positionManager.OnOrderUpdate(ctx, update); err != nil {
        return err
    }

    // 3. Try to persist
    newState, err := e.buildStateSnapshot(ctx)
    if err != nil {
        // ROLLBACK in-memory state
        e.positionManager.RestoreSnapshot(snapshot)
        return fmt.Errorf("failed to build state snapshot: %w", err)
    }

    if err := e.store.SaveState(ctx, newState); err != nil {
        // ROLLBACK in-memory state
        e.positionManager.RestoreSnapshot(snapshot)
        e.logger.Error("Failed to save state, rolled back in-memory changes", "error", err)
        return fmt.Errorf("state persistence failed: %w", err)
    }

    return nil
}

// Add to PositionManager
func (m *PositionManager) CreateSnapshot() *PositionSnapshot {
    m.mu.RLock()
    defer m.mu.RUnlock()

    snapshot := &PositionSnapshot{
        Positions: make(map[PositionKey]Position),
    }
    for k, v := range m.positions {
        snapshot.Positions[k] = *v // Deep copy
    }
    return snapshot
}

func (m *PositionManager) RestoreSnapshot(snapshot *PositionSnapshot) {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.positions = make(map[PositionKey]*Position)
    for k, v := range snapshot.Positions {
        pos := v
        m.positions[k] = &pos
    }
}
```

**Benefits**:
- Guarantees consistency via rollback
- Familiar transaction pattern

**Trade-offs**:
- More complex (snapshot/restore logic)
- Memory overhead (snapshot copy)
- Race condition risk if concurrent updates

---

### Option 3: Write-Ahead Log (Most Robust)

**Effort**: 12-16 hours

```go
type WAL struct {
    mu      sync.Mutex
    file    *os.File
    encoder *json.Encoder
}

func (e *SimpleEngine) processOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
    // 1. Append update to WAL FIRST
    if err := e.wal.Append(update); err != nil {
        return fmt.Errorf("WAL append failed: %w", err)
    }

    // 2. Update in-memory state
    if err := e.positionManager.OnOrderUpdate(ctx, update); err != nil {
        return err
    }

    // 3. Persist full state (async, best-effort)
    go func() {
        newState, _ := e.buildStateSnapshot(context.Background())
        if err := e.store.SaveState(context.Background(), newState); err != nil {
            e.logger.Warn("Background state save failed", "error", err)
            // Not critical - can replay from WAL
        }
    }()

    // 4. Checkpoint WAL periodically
    e.wal.CheckpointIfNeeded()

    return nil
}

// On restart
func (e *SimpleEngine) recoverFromWAL() error {
    // 1. Load last checkpoint from database
    state, err := e.store.LoadState(context.Background())
    if err != nil {
        return err
    }

    // 2. Replay WAL entries since checkpoint
    entries, err := e.wal.ReadSinceCheckpoint(state.CheckpointID)
    if err != nil {
        return err
    }

    for _, entry := range entries {
        e.positionManager.OnOrderUpdate(context.Background(), entry)
    }

    return nil
}
```

**Benefits**:
- Industry-standard approach (Redis, Postgres, SQLite all use WAL)
- Guaranteed durability
- Fast recovery
- Async persistence (no blocking)

**Trade-offs**:
- Most complex implementation
- Requires WAL file management
- Checkpoint logic needed

## Recommended Action

**Implement Option 1** (Persist First):
- Simplest solution that fixes the problem
- No rollback complexity
- Acceptable performance (state snapshots are small)

**Future enhancement**: Consider Option 3 (WAL) if:
- State snapshots become large (>1 MB)
- Persistence latency becomes bottleneck
- Need maximum durability guarantees

## Additional Safety Measures

1. **Add state checksum verification on load**:
```go
func (s *SQLiteStore) LoadState(ctx context.Context) (*pb.State, error) {
    var data string
    var checksum []byte

    row := s.db.QueryRowContext(ctx, "SELECT data, checksum FROM state WHERE id = 1")
    if err := row.Scan(&data, &checksum); err != nil {
        return nil, err
    }

    // Verify checksum
    actualChecksum := sha256.Sum256([]byte(data))
    if !bytes.Equal(checksum, actualChecksum[:]) {
        return nil, fmt.Errorf("state corruption detected: checksum mismatch")
    }

    // Unmarshal
    var state pb.State
    if err := json.Unmarshal([]byte(data), &state); err != nil {
        return nil, err
    }

    return &state, nil
}
```

2. **Add state version tracking**:
```go
type State struct {
    Version   int64  // Monotonically increasing
    Positions map[string]*Position
    // ... other fields
}

// Reject out-of-order updates
if newState.Version <= currentState.Version {
    return fmt.Errorf("state version regression: new=%d, current=%d",
        newState.Version, currentState.Version)
}
```

## Testing

**Persistence failure test**:
```go
func TestStatePersistenceFailure(t *testing.T) {
    // Use mock store that fails on write
    failStore := &FailingStore{}
    engine := NewSimpleEngine(failStore, ...)

    // Get initial position
    initialPos := engine.positionManager.GetPosition("BTC")

    // Process order update
    err := engine.processOrderUpdate(ctx, update)

    // Persistence should fail
    assert.Error(t, err)

    // In-memory position should NOT change
    currentPos := engine.positionManager.GetPosition("BTC")
    assert.Equal(t, initialPos, currentPos, "Position changed despite persistence failure")
}
```

**Crash recovery test**:
```go
func TestCrashRecovery(t *testing.T) {
    // 1. Start engine
    engine := NewSimpleEngine(...)
    engine.processOrderUpdate(ctx, update1)  // Position = 100
    engine.processOrderUpdate(ctx, update2)  // Position = 150

    // 2. Simulate crash (no cleanup)
    engine = nil

    // 3. Restart engine
    newEngine := NewSimpleEngine(...)

    // 4. Verify position matches last persisted state
    pos := newEngine.positionManager.GetPosition("BTC")
    assert.Equal(t, "150", pos.Quantity.String())
}
```

## Acceptance Criteria

- [x] State persisted BEFORE in-memory update
- [x] Persistence failure does NOT change in-memory state
- [x] Crash recovery test passes (loads correct state)
- [x] State checksum verification on load
- [x] State version tracking prevents regression
- [x] All tests pass
- [x] No in-memory/database divergence under any failure mode

## Resources

- Data Integrity Guardian Report: Critical finding #2
- File: `internal/engine/simple/engine.go`
- Related: Issue #007 (SQLite transactions - already resolved)
- Database Design: "Write-Ahead Logging" (Wikipedia)
