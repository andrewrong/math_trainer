package db

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/001_init.sql
var initSQL string

// Open opens (or creates) the SQLite database, runs migrations, and returns
// a *sql.DB ready to use. WAL mode is enabled for better concurrent reads.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite3: %w", err)
	}
	// Single writer for SQLite is fine; allow multiple concurrent readers.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite3: %w", err)
	}
	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	// Check if already migrated to version 1
	var version int
	row := db.QueryRow(`SELECT version FROM schema_migrations WHERE version = 1`)
	err := row.Scan(&version)
	if err == nil {
		return nil // already at v1
	}
	// Run the init SQL (idempotent — all statements use IF NOT EXISTS / OR IGNORE)
	if _, err := db.Exec(initSQL); err != nil {
		return fmt.Errorf("executing init.sql: %w", err)
	}
	return nil
}
