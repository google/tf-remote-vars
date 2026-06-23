# 4. Audit Logging for Configuration Changes

We decided to implement a comprehensive audit log to track all modifications to namespaces, variables, and dependencies.

## Context

Because remote variables directly configure infrastructure, changes (especially deletions or updates) can lead to outages. Having a clear audit trail of "who changed what and when" is critical for debugging, security compliance, and recovery.

## Decision

We will introduce an `audit_logs` table. Every state-changing gRPC operation will write an entry to this table.

### Audit Log Schema

*   `id` (UUID, Primary Key)
*   `timestamp` (TIMESTAMP) - When the action occurred.
*   `actor` (VARCHAR) - The identity of the caller (extracted from gRPC metadata, e.g., service account or user email).
*   `action` (VARCHAR) - The action performed (e.g., `CREATE_NAMESPACE`, `PUT_VARIABLE`, `DELETE_VARIABLE`, `REGISTER_CONSUMER`, `DEREGISTER_CONSUMER`).
*   `target_namespace` (VARCHAR) - The namespace affected.
*   `target_name` (VARCHAR, Nullable) - The variable name affected (if applicable).
*   `old_value` (TEXT, Nullable) - The value before the action (for updates and deletions).
*   `new_value` (TEXT, Nullable) - The value after the action (for creations and updates).
*   `consumer_namespace` (VARCHAR, Nullable) - The consumer namespace (for dependency changes).

### Implementation

The gRPC server will intercept requests, extract the caller's identity from the gRPC context, perform the database operation, and write the audit log entry in the same transaction to ensure consistency.
