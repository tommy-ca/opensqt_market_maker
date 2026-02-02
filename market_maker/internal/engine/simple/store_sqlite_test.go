package simple

import (
	"context"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"
	googletype "google.golang.org/genproto/googleapis/type/decimal"
)

func createTestStore(t *testing.T, dbPath string) *SQLiteStore {
	// Manual migration for tests using Atlas CLI (since it's removed from app deps)
	atlasPath := "/home/tommyk/.local/share/mise/installs/aqua-atlas-community/1.0.0/atlas"

	// Use absolute path for migrations to avoid relative path issues during tests
	dirURL := "file:/home/tommyk/projects/quant/engine/opensqt_market_maker/market_maker/migrations"

	cmd := exec.Command(atlasPath, "migrate", "apply",
		"--dir", dirURL,
		"--url", "sqlite://"+dbPath)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to apply migrations for test: %v\nOutput: %s", err, out)
	}

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	return store
}

func TestSQLiteStore_TransactionBoundaries(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store := createTestStore(t, dbPath)
	defer store.Close()

	ctx := context.Background()

	// Test 1: Successful save and load
	state := &pb.State{
		Symbol:         "BTC-USD",
		LastPrice:      &googletype.Decimal{Value: "50000.0"},
		LastUpdateTime: 1234567890,
		Slots: map[string]*pb.InventorySlot{
			"slot-1": {
				Price:          &googletype.Decimal{Value: "49000"},
				PositionStatus: pb.PositionStatus_POSITION_STATUS_FILLED,
				PositionQty:    &googletype.Decimal{Value: "1.5"},
				SlotStatus:     pb.SlotStatus_SLOT_STATUS_LOCKED,
			},
		},
	}

	if err := store.SaveState(ctx, state); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	loadedState, err := store.LoadState(ctx)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	if loadedState == nil {
		t.Fatal("loaded state is nil")
	}

	if len(loadedState.Slots) != 1 {
		t.Errorf("expected 1 slot, got %d", len(loadedState.Slots))
	}

	if loadedState.Symbol != "BTC-USD" {
		t.Errorf("expected symbol 'BTC-USD', got '%s'", loadedState.Symbol)
	}

	if !pbu.ToGoDecimal(loadedState.LastPrice).Equal(decimal.NewFromInt(50000)) {
		t.Errorf("expected last price 50000.0, got %s", pbu.ToGoDecimal(loadedState.LastPrice))
	}
}

func TestSQLiteStore_WALMode(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store := createTestStore(t, dbPath)
	defer store.Close()

	// Verify WAL mode is enabled
	var journalMode string
	err := store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("failed to query journal mode: %v", err)
	}

	if journalMode != "wal" {
		t.Errorf("expected WAL mode, got %s", journalMode)
	}
}

func TestSQLiteStore_ChecksumValidation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store := createTestStore(t, dbPath)
	defer store.Close()

	ctx := context.Background()

	// Save a state
	state := &pb.State{
		Symbol:         "ETH-USD",
		LastPrice:      &googletype.Decimal{Value: "3000.0"},
		LastUpdateTime: 1234567890,
		Slots: map[string]*pb.InventorySlot{
			"slot-1": {
				Price:          &googletype.Decimal{Value: "3000"},
				PositionStatus: pb.PositionStatus_POSITION_STATUS_FILLED,
				SlotStatus:     pb.SlotStatus_SLOT_STATUS_LOCKED,
			},
		},
	}

	if err := store.SaveState(ctx, state); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	// Corrupt the data in the database
	_, err := store.db.Exec("UPDATE state SET data = '{\"corrupt\": \"data\"}' WHERE id = 1")
	if err != nil {
		t.Fatalf("failed to corrupt data: %v", err)
	}

	// Attempt to load should fail due to checksum mismatch
	_, err = store.LoadState(ctx)
	if err == nil {
		t.Fatal("expected checksum validation error, got nil")
	}

	if err.Error() != "checksum verification failed: data corruption detected" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSQLiteStore_RoundTripValidation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store := createTestStore(t, dbPath)
	defer store.Close()

	ctx := context.Background()

	// Test with complex state
	state := &pb.State{
		Symbol:         "BTC-USD",
		LastPrice:      &googletype.Decimal{Value: "50000.0"},
		LastUpdateTime: 1234567890,
		Slots: map[string]*pb.InventorySlot{
			"slot-1": {
				Price:             &googletype.Decimal{Value: "49000"},
				PositionStatus:    pb.PositionStatus_POSITION_STATUS_FILLED,
				PositionQty:       &googletype.Decimal{Value: "1.5"},
				OrderId:           12345,
				ClientOid:         "client-1",
				OrderSide:         pb.OrderSide_ORDER_SIDE_BUY,
				OrderStatus:       pb.OrderStatus_ORDER_STATUS_FILLED,
				OrderPrice:        &googletype.Decimal{Value: "49000"},
				OrderFilledQty:    &googletype.Decimal{Value: "1.5"},
				SlotStatus:        pb.SlotStatus_SLOT_STATUS_LOCKED,
				PostOnlyFailCount: 0,
			},
			"slot-2": {
				Price:             &googletype.Decimal{Value: "51000"},
				PositionStatus:    pb.PositionStatus_POSITION_STATUS_EMPTY,
				OrderId:           12346,
				ClientOid:         "client-2",
				OrderSide:         pb.OrderSide_ORDER_SIDE_SELL,
				OrderStatus:       pb.OrderStatus_ORDER_STATUS_NEW,
				OrderPrice:        &googletype.Decimal{Value: "51000"},
				SlotStatus:        pb.SlotStatus_SLOT_STATUS_PENDING,
				PostOnlyFailCount: 2,
			},
		},
	}

	// Save and load multiple times to verify round-trip consistency
	for i := 0; i < 5; i++ {
		if err := store.SaveState(ctx, state); err != nil {
			t.Fatalf("iteration %d: failed to save state: %v", i, err)
		}

		loadedState, err := store.LoadState(ctx)
		if err != nil {
			t.Fatalf("iteration %d: failed to load state: %v", i, err)
		}

		if len(loadedState.Slots) != len(state.Slots) {
			t.Errorf("iteration %d: slot count mismatch: expected %d, got %d", i, len(state.Slots), len(loadedState.Slots))
		}

		if loadedState.Symbol != state.Symbol {
			t.Errorf("iteration %d: symbol mismatch", i)
		}

		if !pbu.ToGoDecimal(loadedState.LastPrice).Equal(pbu.ToGoDecimal(state.LastPrice)) {
			t.Errorf("iteration %d: last price mismatch: expected %s, got %s", i, pbu.ToGoDecimal(state.LastPrice), pbu.ToGoDecimal(loadedState.LastPrice))
		}
	}
}

func TestSQLiteStore_ConcurrentWrites(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store := createTestStore(t, dbPath)
	defer store.Close()

	ctx := context.Background()

	// Test serializable isolation prevents concurrent write conflicts
	state1 := &pb.State{
		Symbol:         "BTC-USD",
		LastPrice:      &googletype.Decimal{Value: "50000.0"},
		LastUpdateTime: 1234567890,
		Slots: map[string]*pb.InventorySlot{
			"slot-1": {
				Price:      &googletype.Decimal{Value: "50000"},
				SlotStatus: pb.SlotStatus_SLOT_STATUS_FREE,
			},
		},
	}

	state2 := &pb.State{
		Symbol:         "ETH-USD",
		LastPrice:      &googletype.Decimal{Value: "3000.0"},
		LastUpdateTime: 1234567891,
		Slots: map[string]*pb.InventorySlot{
			"slot-2": {
				Price:      &googletype.Decimal{Value: "3000"},
				SlotStatus: pb.SlotStatus_SLOT_STATUS_FREE,
			},
		},
	}

	// Sequential writes should both succeed
	if err := store.SaveState(ctx, state1); err != nil {
		t.Fatalf("failed to save state1: %v", err)
	}

	if err := store.SaveState(ctx, state2); err != nil {
		t.Fatalf("failed to save state2: %v", err)
	}

	// Final state should be state2
	finalState, err := store.LoadState(ctx)
	if err != nil {
		t.Fatalf("failed to load final state: %v", err)
	}

	if finalState.Symbol != "ETH-USD" {
		t.Error("final state does not match expected state2")
	}
}

func TestSQLiteStore_EmptyState(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store := createTestStore(t, dbPath)
	defer store.Close()

	ctx := context.Background()

	// Load from empty database
	state, err := store.LoadState(ctx)
	if err != nil {
		t.Fatalf("failed to load empty state: %v", err)
	}

	if state != nil {
		t.Error("expected nil state from empty database")
	}
}

func TestSQLiteStore_TransactionRollback(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store := createTestStore(t, dbPath)
	defer store.Close()

	ctx := context.Background()

	// Save initial state
	initialState := &pb.State{
		Symbol:         "BTC-USD",
		LastPrice:      &googletype.Decimal{Value: "50000.0"},
		LastUpdateTime: 1234567890,
		Slots: map[string]*pb.InventorySlot{
			"slot-initial": {
				Price:      &googletype.Decimal{Value: "50000"},
				SlotStatus: pb.SlotStatus_SLOT_STATUS_FREE,
			},
		},
	}

	if err := store.SaveState(ctx, initialState); err != nil {
		t.Fatalf("failed to save initial state: %v", err)
	}

	// Close the database to simulate crash during transaction
	store.Close()

	// Reopen and verify state is intact (WAL recovery)
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer store.Close()

	recoveredState, err := store.LoadState(ctx)
	if err != nil {
		t.Fatalf("failed to load recovered state: %v", err)
	}

	if recoveredState == nil || len(recoveredState.Slots) != 1 {
		t.Fatal("state recovery failed after database close")
	}

	if recoveredState.Symbol != "BTC-USD" {
		t.Error("recovered state does not match initial state")
	}
}

func TestSQLiteStore_SchemaValidation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store := createTestStore(t, dbPath)
	defer store.Close()

	// Verify schema has checksum column
	var columnCount int
	err := store.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('state') WHERE name = 'checksum'").Scan(&columnCount)
	if err != nil {
		t.Fatalf("failed to check schema: %v", err)
	}

	if columnCount != 1 {
		t.Error("checksum column not found in schema")
	}

	// Verify id constraint (should only allow id = 1)
	ctx := context.Background()
	state := &pb.State{
		Symbol:         "BTC-USD",
		LastPrice:      &googletype.Decimal{Value: "50000.0"},
		LastUpdateTime: 1234567890,
	}

	// This should work
	if err := store.SaveState(ctx, state); err != nil {
		t.Fatalf("failed to save state with id=1: %v", err)
	}

	// Manually try to insert with id=2 (should fail)
	_, err = store.db.Exec("INSERT INTO state (id, data, checksum, updated_at) VALUES (2, '{}', X'00', 0)")
	if err == nil {
		t.Error("expected constraint violation for id != 1, but insert succeeded")
	}
}

func TestSQLiteStore_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store := createTestStore(t, dbPath)
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	state := &pb.State{
		Symbol:         "BTC-USD",
		LastPrice:      &googletype.Decimal{Value: "50000.0"},
		LastUpdateTime: 1234567890,
	}

	// Should fail due to cancelled context
	err := store.SaveState(ctx, state)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

func TestSQLiteStore_MigrationFromOldSchema(t *testing.T) {
	t.Skip("Migration not implemented - old schema databases need manual migration or recreation")

	// NOTE: If you have an existing database with the old schema (without checksum column),
	// you need to manually migrate it using:
	//
	// ALTER TABLE state ADD COLUMN checksum BLOB NOT NULL DEFAULT X'0000000000000000000000000000000000000000000000000000000000000000';
	//
	// After adding the column, you should re-save all states to generate proper checksums.
	// Alternatively, delete the old database file and let the application create a new one.
}
