// Package storage implements the durable, local SQLite state of tempo-agent.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	// ErrNoPolicy means that no valid policy has been stored yet.
	ErrNoPolicy = errors.New("no local policy")
	// ErrStalePolicy means an older revision attempted to replace local state.
	ErrStalePolicy = errors.New("policy revision is older than local revision")
)

// Store owns the agent's local SQLite connection.
type Store struct {
	db                    *sql.DB
	policyMu              sync.RWMutex
	cachedPolicy          PolicySnapshot
	hasCachedPolicy       bool
	sessionStateMu        sync.RWMutex
	confirmedSessionState ConfirmedSessionState
	hasConfirmedState     bool
}

// Open opens (or creates) an agent database and applies all embedded migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path cannot be empty")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	dsnURL := url.URL{Scheme: "file", Path: absolutePath}
	query := dsnURL.Query()
	query.Set("_busy_timeout", "5000")
	query.Set("_foreign_keys", "on")
	query.Set("_journal_mode", "WAL")
	query.Set("_synchronous", "FULL")
	dsnURL.RawQuery = query.Encode()

	db, err := sql.Open("sqlite3", dsnURL.String())
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	// A single writer is sufficient for the daemon and makes transaction ordering
	// deterministic. SQLite still permits concurrent readers in WAL mode.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect sqlite database: %w", err)
	}
	if err := s.applyMigrations(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.refreshPolicyCache(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.refreshConfirmedSessionStateLocked(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close flushes SQLite's connection state and closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored timestamp %q: %w", value, err)
	}
	return parsed, nil
}
