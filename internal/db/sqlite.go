// Package db implements the database layer for Energizer, providing
// SQLite-based RBAC (Role-Based Access Control), alert management,
// and schema migrations.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

// Database wraps a SQLite database connection with thread-safe access.
type Database struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

// NewDatabase opens or creates a SQLite database at the given path.
func NewDatabase(dbPath string) (*Database, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database %s: %w", dbPath, err)
	}

	// Configure connection pool for SQLite
	db.SetMaxOpenConns(1) // SQLite doesn't support concurrent writes
	db.SetMaxIdleConns(1)

	// Enable WAL mode for better read concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		log.Warn().Err(err).Msg("failed to enable WAL mode")
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		log.Warn().Err(err).Msg("failed to enable foreign keys")
	}

	// Verify connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	log.Info().Str("path", dbPath).Msg("database opened")

	return &Database{
		db:   db,
		path: dbPath,
	}, nil
}

// Close closes the database connection.
func (d *Database) Close() error {
	return d.db.Close()
}

// Exec executes a query without returning rows (INSERT, UPDATE, DELETE).
func (d *Database) Exec(query string, args ...interface{}) (sql.Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.Exec(query, args...)
}

// Query executes a query that returns rows (SELECT).
func (d *Database) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return d.db.Query(query, args...)
}

// QueryRow executes a query that returns a single row.
func (d *Database) QueryRow(query string, args ...interface{}) *sql.Row {
	return d.db.QueryRow(query, args...)
}

// Transaction executes a function within a database transaction.
func (d *Database) Transaction(fn func(tx *sql.Tx) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}
