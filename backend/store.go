package backend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a resource is not found.
var ErrNotFound = errors.New("not found")

// Namespace represents a namespace in Varlet.
type Namespace struct {
	Name                       string
	RunWebhookURL              string
	RetentionPolicyMinVersions int32
	RetentionPolicyMaxAgeDays  int32
	AllowedConsumers           []string
}

// Variable represents a variable version in Varlet.
type Variable struct {
	Namespace string
	Name      string
	Version   int64
	Value     []byte // Serialized google.protobuf.Value (JSON)
	CreatedAt time.Time
}

// Dependency represents a dependency edge in Varlet.
type Dependency struct {
	Consumer string
	Source   string
	Variable string
}

// AuditLog represents an audit log entry in Varlet.
type AuditLog struct {
	Timestamp time.Time
	Actor     string
	Action    string
	Target    string
	Details   string
}

// Store defines the interface for data persistence.
type Store interface {
	RegisterNamespace(ctx context.Context, ns *Namespace) error
	GetNamespace(ctx context.Context, name string) (*Namespace, error)
	GetNamespaces(ctx context.Context) ([]string, error)
	SetNamespacePolicy(ctx context.Context, namespace string, allowedConsumers []string) error

	// Variables
	PutVariable(ctx context.Context, v *Variable) error
	GetLatestVariable(ctx context.Context, namespace, name string) (*Variable, error)
	DeleteVariable(ctx context.Context, namespace, name string) error
	PruneVariables(ctx context.Context, namespace, name string, minVersions int32, cutoff time.Time) error

	// Dependencies
	RegisterConsumer(ctx context.Context, consumerNS, sourceNS, varName string) error
	DeregisterConsumer(ctx context.Context, consumerNS, sourceNS, varName string) error
	IsConsumer(ctx context.Context, consumerNS, sourceNS, varName string) (bool, error)
	HasConsumers(ctx context.Context, sourceNS, varName string) (bool, error)
	GetDependencies(ctx context.Context, consumerNS string) ([]string, error)
	GetConsumers(ctx context.Context, sourceNS, varName string) ([]string, error)
	GetAllDependencies(ctx context.Context) ([]*Dependency, error)

	// Audit Logs
	WriteAuditLog(ctx context.Context, log *AuditLog) error
	GetAuditLogs(ctx context.Context) ([]*AuditLog, error)

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
	query := `
	CREATE TABLE IF NOT EXISTS namespaces (
		name TEXT PRIMARY KEY,
		run_webhook_url TEXT,
		retention_policy_min_versions INTEGER,
		retention_policy_max_age_days INTEGER
	);
	CREATE TABLE IF NOT EXISTS namespace_policies (
		namespace TEXT,
		allowed_consumer TEXT,
		PRIMARY KEY (namespace, allowed_consumer),
		FOREIGN KEY (namespace) REFERENCES namespaces(name) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS variables (
		namespace TEXT,
		name TEXT,
		version INTEGER,
		value BLOB,
		created_at DATETIME,
		PRIMARY KEY (namespace, name, version),
		FOREIGN KEY (namespace) REFERENCES namespaces(name) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS dependencies (
		consumer_namespace TEXT,
		source_namespace TEXT,
		variable_name TEXT,
		PRIMARY KEY (consumer_namespace, source_namespace, variable_name),
		FOREIGN KEY (consumer_namespace) REFERENCES namespaces(name) ON DELETE CASCADE,
		FOREIGN KEY (source_namespace) REFERENCES namespaces(name) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME,
		actor TEXT,
		action TEXT,
		target TEXT,
		details TEXT
	);`
	if _, err := db.Exec(query); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// RegisterNamespace registers a new namespace.
func (s *SQLiteStore) RegisterNamespace(ctx context.Context, ns *Namespace) error {
	query := `INSERT INTO namespaces (name, run_webhook_url, retention_policy_min_versions, retention_policy_max_age_days) 
              VALUES (?, ?, ?, ?)
              ON CONFLICT(name) DO UPDATE SET
                  run_webhook_url = excluded.run_webhook_url,
                  retention_policy_min_versions = excluded.retention_policy_min_versions,
                  retention_policy_max_age_days = excluded.retention_policy_max_age_days`
	_, err := s.db.ExecContext(ctx, query, ns.Name, ns.RunWebhookURL, ns.RetentionPolicyMinVersions, ns.RetentionPolicyMaxAgeDays)
	if err != nil {
		return fmt.Errorf("failed to register namespace: %w", err)
	}
	return nil
}

// GetNamespace retrieves a namespace by name.
func (s *SQLiteStore) GetNamespace(ctx context.Context, name string) (*Namespace, error) {
	row := s.db.QueryRowContext(ctx, "SELECT name, run_webhook_url, retention_policy_min_versions, retention_policy_max_age_days FROM namespaces WHERE name = ?", name)
	var ns Namespace
	err := row.Scan(&ns.Name, &ns.RunWebhookURL, &ns.RetentionPolicyMinVersions, &ns.RetentionPolicyMaxAgeDays)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get namespace: %w", err)
	}

	// Query policies
	rows, err := s.db.QueryContext(ctx, "SELECT allowed_consumer FROM namespace_policies WHERE namespace = ?", name)
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace policies: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var consumer string
		if err := rows.Scan(&consumer); err != nil {
			return nil, fmt.Errorf("failed to scan allowed consumer: %w", err)
		}
		ns.AllowedConsumers = append(ns.AllowedConsumers, consumer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return &ns, nil
}

// PutVariable stores a new variable version.
func (s *SQLiteStore) PutVariable(ctx context.Context, v *Variable) error {
	query := `INSERT INTO variables (namespace, name, version, value, created_at) VALUES (?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, v.Namespace, v.Name, v.Version, v.Value, v.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to put variable: %w", err)
	}
	return nil
}

// GetLatestVariable retrieves the latest version of a variable.
func (s *SQLiteStore) GetLatestVariable(ctx context.Context, namespace, name string) (*Variable, error) {
	query := `SELECT namespace, name, version, value, created_at FROM variables 
              WHERE namespace = ? AND name = ? 
              ORDER BY version DESC LIMIT 1`
	row := s.db.QueryRowContext(ctx, query, namespace, name)
	var v Variable
	err := row.Scan(&v.Namespace, &v.Name, &v.Version, &v.Value, &v.CreatedAt)
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

func (s *SQLiteStore) RegisterConsumer(ctx context.Context, consumerNS, sourceNS, varName string) error {
	query := `INSERT INTO dependencies (consumer_namespace, source_namespace, variable_name) VALUES (?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, consumerNS, sourceNS, varName)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeregisterConsumer(ctx context.Context, consumerNS, sourceNS, varName string) error {
	query := `DELETE FROM dependencies WHERE consumer_namespace = ? AND source_namespace = ? AND variable_name = ?`
	_, err := s.db.ExecContext(ctx, query, consumerNS, sourceNS, varName)
	if err != nil {
		return fmt.Errorf("failed to deregister consumer: %w", err)
	}
	return nil
}

func (s *SQLiteStore) IsConsumer(ctx context.Context, consumerNS, sourceNS, varName string) (bool, error) {
	query := `SELECT 1 FROM dependencies WHERE consumer_namespace = ? AND source_namespace = ? AND variable_name = ?`
	var dummy int
	err := s.db.QueryRowContext(ctx, query, consumerNS, sourceNS, varName).Scan(&dummy)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check if consumer: %w", err)
	}
	return true, nil
}

func (s *SQLiteStore) HasConsumers(ctx context.Context, sourceNS, varName string) (bool, error) {
	query := `SELECT 1 FROM dependencies WHERE source_namespace = ? AND variable_name = ? LIMIT 1`
	var dummy int
	err := s.db.QueryRowContext(ctx, query, sourceNS, varName).Scan(&dummy)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check if has consumers: %w", err)
	}
	return true, nil
}

func (s *SQLiteStore) GetDependencies(ctx context.Context, consumerNS string) ([]string, error) {
	query := `SELECT DISTINCT source_namespace FROM dependencies WHERE consumer_namespace = ?`
	rows, err := s.db.QueryContext(ctx, query, consumerNS)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependencies: %w", err)
	}
	defer rows.Close()

	var deps []string
	for rows.Next() {
		var dep string
		if err := rows.Scan(&dep); err != nil {
			return nil, fmt.Errorf("failed to scan dependency: %w", err)
		}
		deps = append(deps, dep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return deps, nil
}

func (s *SQLiteStore) SetNamespacePolicy(ctx context.Context, namespace string, allowedConsumers []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, "DELETE FROM namespace_policies WHERE namespace = ?", namespace)
	if err != nil {
		return fmt.Errorf("failed to clear old policies: %w", err)
	}

	for _, consumer := range allowedConsumers {
		_, err = tx.ExecContext(ctx, "INSERT INTO namespace_policies (namespace, allowed_consumer) VALUES (?, ?)", namespace, consumer)
		if err != nil {
			return fmt.Errorf("failed to insert policy: %w", err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) PruneVariables(ctx context.Context, namespace, name string, minVersions int32, cutoff time.Time) error {
	if minVersions > 0 {
		query := `DELETE FROM variables
			WHERE namespace = ? AND name = ?
			  AND created_at < ?
			  AND version NOT IN (
				  SELECT version FROM variables
				  WHERE namespace = ? AND name = ?
				  ORDER BY version DESC
				  LIMIT ?
			  )`
		_, err := s.db.ExecContext(ctx, query, namespace, name, cutoff, namespace, name, minVersions)
		if err != nil {
			return fmt.Errorf("failed to prune variables: %w", err)
		}
	} else {
		query := `DELETE FROM variables
			WHERE namespace = ? AND name = ?
			  AND created_at < ?`
		_, err := s.db.ExecContext(ctx, query, namespace, name, cutoff)
		if err != nil {
			return fmt.Errorf("failed to prune variables: %w", err)
		}
	}
	return nil
}

// GetNamespaces returns all registered namespace names.
func (s *SQLiteStore) GetNamespaces(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT name FROM namespaces ORDER BY name ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to get namespaces: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan namespace name: %w", err)
		}
		names = append(names, name)
	}
	return names, nil
}

// GetAllDependencies returns all dependency edges in the database.
func (s *SQLiteStore) GetAllDependencies(ctx context.Context) ([]*Dependency, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT consumer_namespace, source_namespace, variable_name FROM dependencies ORDER BY consumer_namespace ASC, source_namespace ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to get all dependencies: %w", err)
	}
	defer rows.Close()

	var deps []*Dependency
	for rows.Next() {
		var dep Dependency
		if err := rows.Scan(&dep.Consumer, &dep.Source, &dep.Variable); err != nil {
			return nil, fmt.Errorf("failed to scan dependency: %w", err)
		}
		deps = append(deps, &dep)
	}
	return deps, nil
}

// GetConsumers returns all consumer namespaces for a given source namespace and variable name.
func (s *SQLiteStore) GetConsumers(ctx context.Context, sourceNS, varName string) ([]string, error) {
	query := `SELECT DISTINCT consumer_namespace FROM dependencies WHERE source_namespace = ? AND variable_name = ?`
	rows, err := s.db.QueryContext(ctx, query, sourceNS, varName)
	if err != nil {
		return nil, fmt.Errorf("failed to get consumers: %w", err)
	}
	defer rows.Close()

	var consumers []string
	for rows.Next() {
		var consumer string
		if err := rows.Scan(&consumer); err != nil {
			return nil, fmt.Errorf("failed to scan consumer: %w", err)
		}
		consumers = append(consumers, consumer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return consumers, nil
}

// WriteAuditLog writes an audit log entry.
func (s *SQLiteStore) WriteAuditLog(ctx context.Context, log *AuditLog) error {
	query := `INSERT INTO audit_logs (timestamp, actor, action, target, details) VALUES (?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, log.Timestamp, log.Actor, log.Action, log.Target, log.Details)
	if err != nil {
		return fmt.Errorf("failed to write audit log: %w", err)
	}
	return nil
}

// GetAuditLogs retrieves all audit log entries, ordered by timestamp ascending.
func (s *SQLiteStore) GetAuditLogs(ctx context.Context) ([]*AuditLog, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT timestamp, actor, action, target, details FROM audit_logs ORDER BY timestamp ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to get audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var l AuditLog
		if err := rows.Scan(&l.Timestamp, &l.Actor, &l.Action, &l.Target, &l.Details); err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}
		logs = append(logs, &l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return logs, nil
}

