package simple

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"market_maker/internal/pb"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Enable WAL mode for crash recovery
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// NOTE: Database schema must be managed via Atlas CLI migrations.
	// Ensure 'atlas migrate apply' is run before starting the application.

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) SaveState(ctx context.Context, state *pb.State) error {
	// Start transaction with serializable isolation
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// Marshal state
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Validate JSON (round-trip test)
	var testState pb.State
	if err := json.Unmarshal(data, &testState); err != nil {
		return fmt.Errorf("state validation failed: %w", err)
	}

	// Save with checksum
	checksum := sha256.Sum256(data)
	query := `INSERT OR REPLACE INTO state (id, data, checksum, updated_at) VALUES (1, ?, ?, ?)`
	_, err = tx.ExecContext(ctx, query, string(data), checksum[:], time.Now().UnixNano())
	if err != nil {
		return fmt.Errorf("failed to write state to db: %w", err)
	}

	// Atomic commit
	return tx.Commit()
}

func (s *SQLiteStore) LoadState(ctx context.Context) (*pb.State, error) {
	query := `SELECT data, checksum FROM state WHERE id = 1`
	var data string
	var storedChecksum []byte
	err := s.db.QueryRowContext(ctx, query).Scan(&data, &storedChecksum)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read state from db: %w", err)
	}

	// Verify checksum
	computedChecksum := sha256.Sum256([]byte(data))
	if len(storedChecksum) != len(computedChecksum) {
		return nil, fmt.Errorf("checksum length mismatch: expected %d, got %d", len(computedChecksum), len(storedChecksum))
	}
	for i := range computedChecksum {
		if storedChecksum[i] != computedChecksum[i] {
			return nil, fmt.Errorf("checksum verification failed: data corruption detected")
		}
	}

	var state pb.State
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return &state, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
