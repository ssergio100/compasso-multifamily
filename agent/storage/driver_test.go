package storage

import (
	"net/url"
	"path/filepath"
	"testing"
)

func TestSQLiteDataSourceKeepsRequiredPragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent state.db")
	dsn := sqliteDataSource(path)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "file" || parsed.Query().Get("_busy_timeout") != "5000" ||
		parsed.Query().Get("_foreign_keys") != "on" || parsed.Query().Get("_journal_mode") != "WAL" ||
		parsed.Query().Get("_synchronous") != "FULL" {
		t.Fatalf("unexpected SQLite DSN %q", dsn)
	}
}
