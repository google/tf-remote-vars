package backend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a resource is not found.
var ErrNotFound = errors.New("not found")

// Namespace represents a namespace in Varlet.
type Namespace struct {
	Name string
}

// Store defines the interface for data persistence.
type Store interface {
	RegisterNamespace(ctx context.Context, ns *Namespace) error
	GetNamespace(ctx context.Context, name string) (*Namespace, error)
	Close() error
}

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLiteStore.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create tables if they don't exist.
	// ponytail: only name column for now, will add retention and webhook in Slice 4.
	query := `
	CREATE TABLE IF NOT EXISTS namespaces (
		name TEXT PRIMARY KEY
	);`
	if _, err := db.Exec(query); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create namespaces table: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// RegisterNamespace registers a new namespace.
func (s *SQLiteStore) RegisterNamespace(ctx context.Context, ns *Namespace) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO namespaces (name) VALUES (?)", ns.Name)
	if err != nil {
		return fmt.Errorf("failed to register namespace: %w", err)
	}
	return nil
}

// GetNamespace retrieves a namespace by name.
func (s *SQLiteStore) GetNamespace(ctx context.Context, name string) (*Namespace, error) {
	row := s.db.QueryRowContext(ctx, "SELECT name FROM namespaces WHERE name = ?", name)
	var ns Namespace
	err := row.Scan(&ns.Name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get namespace: %w", err)
	}
	return &ns, nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
