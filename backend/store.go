package backend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
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
	WebhookDelayMinutes        int32
	DedupDelayMinutes          int32
	MaxDedupChanges            int32
	AllowedConsumers           []string
}

// Variable represents a variable version in Varlet.
type Variable struct {
	Namespace     string
	Name          string
	Version       int64
	Value         []byte // Serialized google.protobuf.Value (JSON)
	CreatedAt     time.Time
	ActuationUUID string
}

// Actuation represents an actuation run.
type ActuationStatus string

const (
	ActuationStatusTriggered ActuationStatus = "triggered"
	ActuationStatusActuating ActuationStatus = "actuating"
	ActuationStatusCompleted ActuationStatus = "completed"
	ActuationStatusStale     ActuationStatus = "stale"
	ActuationStatusFailed    ActuationStatus = "failed"
)

type Actuation struct {
	UUID      string
	Namespace string
	Source    string // "organic" or "webhook"
	Status    ActuationStatus
	CreatedAt time.Time
}

// PendingWebhookInfo represents a queued webhook.
type PendingWebhookInfo struct {
	ConsumerNamespace string
	TriggerUUID       string
	FireAt            time.Time
}

// Dependency represents a dependency edge in Varlet.
type Dependency struct {
	Consumer string
	Source   string
	Variable string
}

// LineageEdge represents an edge in the actuation ancestry trace.
type LineageEdge struct {
	ChildUUID     string
	ParentUUID    string
	VariableNames []string
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
	HasVariables(ctx context.Context, namespace string) (bool, error)

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

	// Webhook Queue
	QueueWebhook(ctx context.Context, consumerNS string, webhookDelay, dedupDelay int32, triggerUUID string, parentUUID string, now time.Time) error
	GetPendingWebhooksToFire(ctx context.Context, now time.Time) ([]*PendingWebhookInfo, error)
	GetPendingWebhookParents(ctx context.Context, triggerUUID string) ([]string, error)
	RemovePendingWebhook(ctx context.Context, consumerNS string) error

	// Actuations
	CreateActuation(ctx context.Context, act *Actuation, parentUUIDs []string) error
	UpdateActuationStatus(ctx context.Context, uuid string, status ActuationStatus) error
	GetActuation(ctx context.Context, uuid string) (*Actuation, error)
	GetActuationParents(ctx context.Context, uuid string) ([]string, error)
	GetLastActuation(ctx context.Context, namespace string) (*Actuation, error)
	GetActuationTrace(ctx context.Context, startUUID string) ([]*Actuation, []*LineageEdge, error)
	GetUnnotifiedRootActuations(ctx context.Context) ([]*Actuation, error)
	IsCascadeComplete(ctx context.Context, rootUUID string) (bool, error)
	SetActuationNotified(ctx context.Context, uuid string, status ActuationStatus) error

	// Affected namespaces tracking
	RecordAffectedNamespace(ctx context.Context, ns string, causalUUIDs []string) error
	ClearAffectedNamespace(ctx context.Context, ns string) error
	GetCausalActuationUUIDs(ctx context.Context, ns string) ([]string, error)

	// Completion Hooks
	RegisterCompletionHook(ctx context.Context, url string) error
	DeregisterCompletionHook(ctx context.Context, url string) error
	ListCompletionHooks(ctx context.Context) ([]string, error)

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
		retention_policy_max_age_days INTEGER,
		webhook_delay_minutes INTEGER DEFAULT 0,
		dedup_delay_minutes INTEGER DEFAULT 0,
		max_dedup_changes INTEGER DEFAULT 0
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
		actuation_uuid TEXT,
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
	);
	CREATE TABLE IF NOT EXISTS pending_webhooks (
		consumer_namespace TEXT PRIMARY KEY,
		fire_at DATETIME NOT NULL,
		trigger_uuid TEXT NOT NULL,
		FOREIGN KEY (consumer_namespace) REFERENCES namespaces(name) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS pending_webhook_parents (
		trigger_uuid TEXT NOT NULL,
		parent_actuation_uuid TEXT NOT NULL,
		PRIMARY KEY (trigger_uuid, parent_actuation_uuid)
	);
	CREATE TABLE IF NOT EXISTS actuations (
		uuid TEXT PRIMARY KEY,
		namespace TEXT NOT NULL,
		source TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active',
		completion_notified INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL,
		FOREIGN KEY (namespace) REFERENCES namespaces(name) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS actuation_lineage (
		actuation_uuid TEXT NOT NULL,
		parent_actuation_uuid TEXT NOT NULL,
		PRIMARY KEY (actuation_uuid, parent_actuation_uuid),
		FOREIGN KEY (actuation_uuid) REFERENCES actuations(uuid) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS affected_namespaces (
		namespace TEXT NOT NULL,
		causal_actuation_uuid TEXT NOT NULL,
		PRIMARY KEY (namespace, causal_actuation_uuid),
		FOREIGN KEY (namespace) REFERENCES namespaces(name) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS completion_hooks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT UNIQUE NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(query); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	// Add missing columns if they don't exist in existing DB
	_, _ = db.Exec("ALTER TABLE namespaces ADD COLUMN dedup_delay_minutes INTEGER DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE namespaces ADD COLUMN max_dedup_changes INTEGER DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE variables ADD COLUMN actuation_uuid TEXT;")
	_, _ = db.Exec("ALTER TABLE actuations ADD COLUMN completion_notified INTEGER DEFAULT 0;")

	return &SQLiteStore{db: db}, nil
}

// RegisterNamespace registers a new namespace.
func (s *SQLiteStore) RegisterNamespace(ctx context.Context, ns *Namespace) error {
	query := `INSERT INTO namespaces (name, run_webhook_url, retention_policy_min_versions, retention_policy_max_age_days, webhook_delay_minutes, dedup_delay_minutes, max_dedup_changes) 
              VALUES (?, ?, ?, ?, ?, ?, ?)
              ON CONFLICT(name) DO UPDATE SET
                  run_webhook_url = excluded.run_webhook_url,
                  retention_policy_min_versions = excluded.retention_policy_min_versions,
                  retention_policy_max_age_days = excluded.retention_policy_max_age_days,
                  webhook_delay_minutes = excluded.webhook_delay_minutes,
                  dedup_delay_minutes = excluded.dedup_delay_minutes,
                  max_dedup_changes = excluded.max_dedup_changes`
	_, err := s.db.ExecContext(ctx, query, ns.Name, ns.RunWebhookURL, ns.RetentionPolicyMinVersions, ns.RetentionPolicyMaxAgeDays, ns.WebhookDelayMinutes, ns.DedupDelayMinutes, ns.MaxDedupChanges)
	if err != nil {
		return fmt.Errorf("failed to register namespace: %w", err)
	}
	return nil
}

// GetNamespace retrieves a namespace by name.
func (s *SQLiteStore) GetNamespace(ctx context.Context, name string) (*Namespace, error) {
	row := s.db.QueryRowContext(ctx, "SELECT name, run_webhook_url, retention_policy_min_versions, retention_policy_max_age_days, webhook_delay_minutes, dedup_delay_minutes, max_dedup_changes FROM namespaces WHERE name = ?", name)
	var ns Namespace
	err := row.Scan(&ns.Name, &ns.RunWebhookURL, &ns.RetentionPolicyMinVersions, &ns.RetentionPolicyMaxAgeDays, &ns.WebhookDelayMinutes, &ns.DedupDelayMinutes, &ns.MaxDedupChanges)
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `INSERT INTO variables (namespace, name, version, value, created_at, actuation_uuid) VALUES (?, ?, ?, ?, ?, ?)`
	var actUUID any = nil
	if v.ActuationUUID != "" {
		actUUID = v.ActuationUUID
		// Ensure parent/active actuation is recorded in the database
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO actuations (uuid, namespace, source, status, created_at) 
			VALUES (?, ?, 'organic', 'completed', ?)`, v.ActuationUUID, v.Namespace, v.CreatedAt)
		if err != nil {
			return fmt.Errorf("failed to ensure actuation exists for variable: %w", err)
		}
	}
	_, err = tx.ExecContext(ctx, query, v.Namespace, v.Name, v.Version, v.Value, v.CreatedAt, actUUID)
	if err != nil {
		return fmt.Errorf("failed to put variable: %w", err)
	}
	return tx.Commit()
}

// GetLatestVariable retrieves the latest version of a variable.
func (s *SQLiteStore) GetLatestVariable(ctx context.Context, namespace, name string) (*Variable, error) {
	query := `SELECT namespace, name, version, value, created_at, actuation_uuid FROM variables 
              WHERE namespace = ? AND name = ? 
              ORDER BY version DESC LIMIT 1`
	row := s.db.QueryRowContext(ctx, query, namespace, name)
	var v Variable
	var actUUID sql.NullString
	err := row.Scan(&v.Namespace, &v.Name, &v.Version, &v.Value, &v.CreatedAt, &actUUID)
	if err == nil {
		if actUUID.Valid {
			v.ActuationUUID = actUUID.String
		}
	}
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

func (s *SQLiteStore) HasVariables(ctx context.Context, namespace string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM variables WHERE namespace = ?)`
	var exists bool
	err := s.db.QueryRowContext(ctx, query, namespace).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check if namespace has variables: %w", err)
	}
	return exists, nil
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

func (s *SQLiteStore) QueueWebhook(ctx context.Context, consumerNS string, webhookDelay, dedupDelay int32, triggerUUID string, parentUUID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Check if already queued
	var existingUUID string
	var existingFireAt time.Time
	row := tx.QueryRowContext(ctx, "SELECT trigger_uuid, fire_at FROM pending_webhooks WHERE consumer_namespace = ?", consumerNS)
	err = row.Scan(&existingUUID, &existingFireAt)
	isNotFound := errors.Is(err, sql.ErrNoRows)
	if err != nil && !isNotFound {
		return fmt.Errorf("failed to check pending webhook: %w", err)
	}

	activeUUID := triggerUUID
	var calculatedFireAt time.Time
	if isNotFound {
		// First change
		delay := webhookDelay
		if dedupDelay > delay {
			delay = dedupDelay
		}
		calculatedFireAt = now.Add(time.Duration(delay) * time.Minute)

		_, err = tx.ExecContext(ctx, "INSERT INTO pending_webhooks (consumer_namespace, fire_at, trigger_uuid) VALUES (?, ?, ?)", consumerNS, calculatedFireAt, triggerUUID)
		if err != nil {
			return fmt.Errorf("failed to insert pending webhook: %w", err)
		}
	} else {
		// Subsequent change - sliding window
		activeUUID = existingUUID
		calculatedFireAt = now.Add(time.Duration(dedupDelay) * time.Minute)
		if calculatedFireAt.After(existingFireAt) {
			_, err = tx.ExecContext(ctx, "UPDATE pending_webhooks SET fire_at = ? WHERE consumer_namespace = ?", calculatedFireAt, consumerNS)
			if err != nil {
				return fmt.Errorf("failed to update pending webhook fire_at: %w", err)
			}
		} else {
			calculatedFireAt = existingFireAt
		}
	}

	// Insert parent association if parentUUID is provided
	if parentUUID != "" {
		_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO pending_webhook_parents (trigger_uuid, parent_actuation_uuid) VALUES (?, ?)", activeUUID, parentUUID)
		if err != nil {
			return fmt.Errorf("failed to insert pending webhook parent: %w", err)
		}
	}

	// Check max_dedup_changes
	var maxChanges int32
	err = tx.QueryRowContext(ctx, "SELECT max_dedup_changes FROM namespaces WHERE name = ?", consumerNS).Scan(&maxChanges)
	if err == nil && maxChanges > 0 {
		var parentCount int
		err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pending_webhook_parents WHERE trigger_uuid = ?", activeUUID).Scan(&parentCount)
		if err == nil && parentCount >= int(maxChanges) {
			// Force immediate fire by setting fire_at to now
			_, err = tx.ExecContext(ctx, "UPDATE pending_webhooks SET fire_at = ? WHERE consumer_namespace = ?", now, consumerNS)
			if err != nil {
				return fmt.Errorf("failed to force fire pending webhook: %w", err)
			}
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) GetPendingWebhooksToFire(ctx context.Context, now time.Time) ([]*PendingWebhookInfo, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT consumer_namespace, trigger_uuid, fire_at FROM pending_webhooks WHERE fire_at <= ?", now)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending webhooks: %w", err)
	}
	defer rows.Close()

	var infos []*PendingWebhookInfo
	for rows.Next() {
		var info PendingWebhookInfo
		if err := rows.Scan(&info.ConsumerNamespace, &info.TriggerUUID, &info.FireAt); err != nil {
			return nil, fmt.Errorf("failed to scan pending webhook: %w", err)
		}
		infos = append(infos, &info)
	}
	return infos, nil
}

func (s *SQLiteStore) GetPendingWebhookParents(ctx context.Context, triggerUUID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT parent_actuation_uuid FROM pending_webhook_parents WHERE trigger_uuid = ?", triggerUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending webhook parents: %w", err)
	}
	defer rows.Close()

	var parents []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("failed to scan pending webhook parent: %w", err)
		}
		parents = append(parents, p)
	}
	return parents, nil
}

func (s *SQLiteStore) RemovePendingWebhook(ctx context.Context, consumerNS string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Get trigger UUID first
	var triggerUUID string
	err = tx.QueryRowContext(ctx, "SELECT trigger_uuid FROM pending_webhooks WHERE consumer_namespace = ?", consumerNS).Scan(&triggerUUID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // Already removed or not exists
		}
		return fmt.Errorf("failed to get trigger uuid: %w", err)
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM pending_webhooks WHERE consumer_namespace = ?", consumerNS)
	if err != nil {
		return fmt.Errorf("failed to delete pending webhook: %w", err)
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM pending_webhook_parents WHERE trigger_uuid = ?", triggerUUID)
	if err != nil {
		return fmt.Errorf("failed to delete pending webhook parents: %w", err)
	}

	return tx.Commit()
}

func (s *SQLiteStore) CreateActuation(ctx context.Context, act *Actuation, parentUUIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Use INSERT OR IGNORE in case it was pre-created during webhook triggering
	query := `INSERT INTO actuations (uuid, namespace, source, status, created_at) 
              VALUES (?, ?, ?, ?, ?)
              ON CONFLICT(uuid) DO UPDATE SET 
                  status = excluded.status`
	_, err = tx.ExecContext(ctx, query, act.UUID, act.Namespace, act.Source, act.Status, act.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert/update actuation: %w", err)
	}

	for _, p := range parentUUIDs {
		_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO actuation_lineage (actuation_uuid, parent_actuation_uuid) VALUES (?, ?)", act.UUID, p)
		if err != nil {
			return fmt.Errorf("failed to insert actuation lineage: %w", err)
		}
	}

	// Clear affected namespace in DB atomically to prevent race condition during cascade completion evaluation
	_, err = tx.ExecContext(ctx, "DELETE FROM affected_namespaces WHERE namespace = ?", act.Namespace)
	if err != nil {
		return fmt.Errorf("failed to clear affected namespaces: %w", err)
	}

	return tx.Commit()
}

func (s *SQLiteStore) UpdateActuationStatus(ctx context.Context, uuid string, status ActuationStatus) error {
	_, err := s.db.ExecContext(ctx, "UPDATE actuations SET status = ? WHERE uuid = ?", status, uuid)
	if err != nil {
		return fmt.Errorf("failed to update actuation status: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetActuation(ctx context.Context, uuid string) (*Actuation, error) {
	row := s.db.QueryRowContext(ctx, "SELECT uuid, namespace, source, status, created_at FROM actuations WHERE uuid = ?", uuid)
	var act Actuation
	err := row.Scan(&act.UUID, &act.Namespace, &act.Source, &act.Status, &act.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get actuation: %w", err)
	}
	return &act, nil
}

func (s *SQLiteStore) GetActuationParents(ctx context.Context, uuid string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT parent_actuation_uuid FROM actuation_lineage WHERE actuation_uuid = ?", uuid)
	if err != nil {
		return nil, fmt.Errorf("failed to query actuation parents: %w", err)
	}
	defer rows.Close()

	var parents []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("failed to scan parent UUID: %w", err)
		}
		parents = append(parents, p)
	}
	return parents, nil
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

func (s *SQLiteStore) RecordAffectedNamespace(ctx context.Context, ns string, causalUUIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, u := range causalUUIDs {
		_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO affected_namespaces (namespace, causal_actuation_uuid) VALUES (?, ?)", ns, u)
		if err != nil {
			return fmt.Errorf("failed to insert affected namespace: %w", err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ClearAffectedNamespace(ctx context.Context, ns string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM affected_namespaces WHERE namespace = ?", ns)
	if err != nil {
		return fmt.Errorf("failed to clear affected namespaces: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetCausalActuationUUIDs(ctx context.Context, ns string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT causal_actuation_uuid FROM affected_namespaces WHERE namespace = ?", ns)
	if err != nil {
		return nil, fmt.Errorf("failed to query causal actuation UUIDs: %w", err)
	}
	defer rows.Close()

	var uuids []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, fmt.Errorf("failed to scan causal actuation UUID: %w", err)
		}
		uuids = append(uuids, u)
	}
	return uuids, nil
}

func (s *SQLiteStore) GetLastActuation(ctx context.Context, namespace string) (*Actuation, error) {
	query := `SELECT uuid, namespace, source, status, created_at FROM actuations 
              WHERE namespace = ? AND status = 'completed'
              ORDER BY created_at DESC LIMIT 1`
	row := s.db.QueryRowContext(ctx, query, namespace)
	var act Actuation
	err := row.Scan(&act.UUID, &act.Namespace, &act.Source, &act.Status, &act.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get last actuation: %w", err)
	}
	return &act, nil
}

func (s *SQLiteStore) GetActuationTrace(ctx context.Context, startUUID string) ([]*Actuation, []*LineageEdge, error) {
	queryNodes := `
	WITH RECURSIVE ancestors(uuid) AS (
		SELECT ? AS uuid
		UNION
		SELECT parent_actuation_uuid FROM actuation_lineage l
		JOIN ancestors a ON l.actuation_uuid = a.uuid
	)
	SELECT uuid, namespace, source, status, created_at FROM actuations
	WHERE uuid IN ancestors`

	rows, err := s.db.QueryContext(ctx, queryNodes, startUUID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query trace nodes: %w", err)
	}

	var nodes []*Actuation
	nodeMap := make(map[string]*Actuation)
	var uuidList []any

	err = func() error {
		defer rows.Close()
		for rows.Next() {
			var act Actuation
			err := rows.Scan(&act.UUID, &act.Namespace, &act.Source, &act.Status, &act.CreatedAt)
			if err != nil {
				return fmt.Errorf("failed to scan trace node: %w", err)
			}
			nodes = append(nodes, &act)
			nodeMap[act.UUID] = &act
			uuidList = append(uuidList, act.UUID)
		}
		return rows.Err()
	}()
	if err != nil {
		return nil, nil, err
	}

	if len(nodes) == 0 {
		return nil, nil, ErrNotFound
	}

	inPlaceholders := ""
	for i := 0; i < len(uuidList); i++ {
		if i > 0 {
			inPlaceholders += ","
		}
		inPlaceholders += "?"
	}

	queryEdges := fmt.Sprintf(`
		SELECT actuation_uuid, parent_actuation_uuid FROM actuation_lineage
		WHERE actuation_uuid IN (%s) AND parent_actuation_uuid IN (%s)`, inPlaceholders, inPlaceholders)

	args := append(uuidList, uuidList...)
	edgeRows, err := s.db.QueryContext(ctx, queryEdges, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query trace edges: %w", err)
	}

	type rawEdge struct {
		child  string
		parent string
	}
	var rawEdges []rawEdge

	err = func() error {
		defer edgeRows.Close()
		for edgeRows.Next() {
			var child, parent string
			if err := edgeRows.Scan(&child, &parent); err != nil {
				return fmt.Errorf("failed to scan trace edge: %w", err)
			}
			rawEdges = append(rawEdges, rawEdge{child: child, parent: parent})
		}
		return edgeRows.Err()
	}()
	if err != nil {
		return nil, nil, err
	}

	var edges []*LineageEdge
	for _, re := range rawEdges {
		childNode := nodeMap[re.child]
		if childNode == nil {
			continue
		}

		parentNode := nodeMap[re.parent]
		var vars []string
		if parentNode != nil {
			queryVars := `
				SELECT DISTINCT v.name FROM variables v
				JOIN dependencies d ON v.namespace = d.source_namespace AND v.name = d.variable_name
				WHERE v.actuation_uuid = ? AND d.consumer_namespace = ?`
			varRows, err := s.db.QueryContext(ctx, queryVars, re.parent, childNode.Namespace)
			if err == nil {
				err = func() error {
					defer varRows.Close()
					for varRows.Next() {
						var vName string
						if err := varRows.Scan(&vName); err == nil {
							vars = append(vars, vName)
						}
					}
					return varRows.Err()
				}()
				if err != nil {
					log.Printf("[WARNING] failed to scan variables for trace edge: %v", err)
				}
			} else {
				log.Printf("[WARNING] failed to query variables for trace edge: %v", err)
			}
		}

		edges = append(edges, &LineageEdge{
			ChildUUID:     re.child,
			ParentUUID:    re.parent,
			VariableNames: vars,
		})
	}

	return nodes, edges, nil
}

// RegisterCompletionHook registers a completion hook URL in the database.
func (s *SQLiteStore) RegisterCompletionHook(ctx context.Context, url string) error {
	_, err := s.db.ExecContext(ctx, "INSERT OR IGNORE INTO completion_hooks (url) VALUES (?);", url)
	if err != nil {
		return fmt.Errorf("failed to register completion hook: %w", err)
	}
	return nil
}

// DeregisterCompletionHook removes a registered completion hook URL from the database.
func (s *SQLiteStore) DeregisterCompletionHook(ctx context.Context, url string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM completion_hooks WHERE url = ?;", url)
	if err != nil {
		return fmt.Errorf("failed to deregister completion hook: %w", err)
	}
	return nil
}

// ListCompletionHooks returns all registered hook URLs.
func (s *SQLiteStore) ListCompletionHooks(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT url FROM completion_hooks ORDER BY created_at ASC;")
	if err != nil {
		return nil, fmt.Errorf("failed to query completion hooks: %w", err)
	}
	defer rows.Close()

	var urls []string
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, fmt.Errorf("failed to scan hook URL: %w", err)
		}
		urls = append(urls, url)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading hook URLs: %w", err)
	}
	return urls, nil
}

// GetUnnotifiedRootActuations returns all organic root actuations where completion hook has not been fired yet.
func (s *SQLiteStore) GetUnnotifiedRootActuations(ctx context.Context) ([]*Actuation, error) {
	query := `
		SELECT uuid, namespace, source, status, created_at 
		FROM actuations
		WHERE completion_notified = 0
		  AND uuid NOT IN (SELECT actuation_uuid FROM actuation_lineage);
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query unnotified root actuations: %w", err)
	}
	defer rows.Close()

	var acts []*Actuation
	for rows.Next() {
		var act Actuation
		if err := rows.Scan(&act.UUID, &act.Namespace, &act.Source, &act.Status, &act.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan actuation: %w", err)
		}
		acts = append(acts, &act)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading actuations: %w", err)
	}
	return acts, nil
}

// IsCascadeComplete evaluates if a root UUID's cascade is completely finished.
func (s *SQLiteStore) IsCascadeComplete(ctx context.Context, rootUUID string) (bool, error) {
	query := `
		WITH RECURSIVE descendants(uuid) AS (
			SELECT ? AS uuid
			UNION ALL
			SELECT l.actuation_uuid
			FROM actuation_lineage l
			JOIN descendants d ON l.parent_actuation_uuid = d.uuid
		)
		SELECT 
			(SELECT COUNT(*) FROM actuations WHERE uuid IN (SELECT uuid FROM descendants) AND status IN ('actuating', 'triggered')) AS active_count,
			(SELECT COUNT(*) FROM affected_namespaces WHERE causal_actuation_uuid IN (SELECT uuid FROM descendants)) AS affected_count,
			(SELECT COUNT(*) FROM pending_webhook_parents WHERE parent_actuation_uuid IN (SELECT uuid FROM descendants)) AS pending_count;
	`
	var activeCount, affectedCount, pendingCount int
	err := s.db.QueryRowContext(ctx, query, rootUUID).Scan(&activeCount, &affectedCount, &pendingCount)
	if err != nil {
		return false, fmt.Errorf("failed to check cascade completion: %w", err)
	}
	return (activeCount + affectedCount + pendingCount) == 0, nil
}

// SetActuationNotified marks the root actuation status as completed/stale and notified.
func (s *SQLiteStore) SetActuationNotified(ctx context.Context, uuid string, status ActuationStatus) error {
	_, err := s.db.ExecContext(ctx, "UPDATE actuations SET status = ?, completion_notified = 1 WHERE uuid = ?;", status, uuid)
	if err != nil {
		return fmt.Errorf("failed to set actuation notified: %w", err)
	}
	return nil
}


