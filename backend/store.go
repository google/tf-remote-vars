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

// Variable represents a variable version in Varlet.
type Variable struct {
	Namespace string
	Name      string
	Version   int64
	Value     []byte // Serialized google.protobuf.Value (JSON)
}

// Store defines the interface for data persistence.
type Store interface {
	RegisterNamespace(ctx context.Context, ns *Namespace) error
	GetNamespace(ctx context.Context, name string) (*Namespace, error)

	// Variables
	PutVariable(ctx context.Context, v *Variable) error
	GetLatestVariable(ctx context.Context, namespace, name string) (*Variable, error)
	DeleteVariable(ctx context.Context, namespace, name string) error

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

	db.SetMaxOpenConns(1)

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Set busy timeout to avoid SQLITE_BUSY errors during concurrent tests
	if _, err := db.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Create tables if they don't exist.
	// ponytail: only name column for now, will add retention and webhook in Slice 4.
	query := `
	CREATE TABLE IF NOT EXISTS namespaces (
		name TEXT PRIMARY KEY
	);
	CREATE TABLE IF NOT EXISTS variables (
		namespace TEXT,
		name TEXT,
		version INTEGER,
		value BLOB,
		PRIMARY KEY (namespace, name, version),
		FOREIGN KEY (namespace) REFERENCES namespaces(name) ON DELETE CASCADE
	);`
	if _, err := db.Exec(query); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
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

// PutVariable stores a new variable version.
func (s *SQLiteStore) PutVariable(ctx context.Context, v *Variable) error {
	query := `INSERT INTO variables (namespace, name, version, value) VALUES (?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, v.Namespace, v.Name, v.Version, v.Value)
	if err != nil {
		return fmt.Errorf("failed to put variable: %w", err)
	}
	return nil
}

// GetLatestVariable retrieves the latest version of a variable.
func (s *SQLiteStore) GetLatestVariable(ctx context.Context, namespace, name string) (*Variable, error) {
	query := `SELECT namespace, name, version, value FROM variables 
              WHERE namespace = ? AND name = ? 
              ORDER BY version DESC LIMIT 1`
	row := s.db.QueryRowContext(ctx, query, namespace, name)
	var v Variable
	err := row.Scan(&v.Namespace, &v.Name, &v.Version, &v.Value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get latest variable: %w", err)
	}
	return &v, nil
}

// DeleteVariable deletes all versions of a variable.
func (s *SQLiteStore) DeleteVariable(ctx context.Context, namespace, name string) error {
	query := `DELETE FROM variables WHERE namespace = ? AND name = ?`
	_, err := s.db.ExecContext(ctx, query, namespace, name)
	if err != nil {
		return fmt.Errorf("failed to delete variable: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

