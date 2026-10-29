// Package db stores the ROM catalog in SQLite (pure-Go driver, no CGO).
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct {
	conn *sql.DB
}

// Each entry is one schema version. Never edit a released migration:
// append a new one instead.
var migrations = [][]string{
	{ // v1
		`CREATE TABLE IF NOT EXISTS roms (
			id        TEXT PRIMARY KEY,           -- SHA-1 of the content
			system    TEXT NOT NULL,
			name      TEXT NOT NULL,
			path      TEXT NOT NULL UNIQUE,
			size      INTEGER NOT NULL,
			mtime     INTEGER NOT NULL,           -- lets sync skip rehashing unchanged files
			language  TEXT NOT NULL DEFAULT '',
			added_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_roms_system ON roms(system)`,
	},
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}
	conn, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}
	conn.SetMaxOpenConns(1) // SQLite: a single connection avoids "database is locked"
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}
	s := &Store{conn: conn}
	if err := s.migrate(); err != nil {
		conn.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	var version int
	if err := s.conn.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("reading database version: %w", err)
	}
	for i := version; i < len(migrations); i++ {
		for _, stmt := range migrations[i] {
			if _, err := s.conn.Exec(stmt); err != nil {
				return fmt.Errorf("migration v%d: %w", i+1, err)
			}
		}
		if _, err := s.conn.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			return fmt.Errorf("writing database version: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error { return s.conn.Close() }
