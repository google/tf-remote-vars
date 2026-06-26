# Implementation Plan: Varlet Issues

This document breaks down the implementation of Varlet into vertical slices (tracer bullets). Each slice is a demoable/verifiable increment.

## Slices Breakdown

### Slice 0: Project Scaffolding & Code Gen [DONE]
Setup the basic project structure and generate Go code from the proto definition.
*   **Tasks:**
    *   [x] Initialize Go module (`github.com/google/varlet`).
    *   [x] Verify proto compilation and generate Go gRPC stubs from `proto/v1/varlet.proto`.
*   **Acceptance Criteria:**
    *   [x] Go code generates successfully without errors.
*   **Blocked by:** None

### Slice 1: Namespace (Basic) End-to-End [DONE]
Enable basic namespace registration and validation.
*   **Tasks:**
    *   [x] **Backend:** Define `Store` interface for Namespace. Implement SQLite store for namespaces. Implement `RegisterNamespace` and `GetNamespace` gRPC handlers.
    *   [x] **Provider:** Create Terraform provider scaffolding. Implement `varlet_namespace` data source.
*   **Acceptance Criteria:**
    *   [x] Terraform `plan` succeeds when referencing an existing namespace via `data "varlet_namespace"`.
    *   [x] Terraform `plan` fails with clear error if namespace does not exist.
*   **Blocked by:** Slice 0

### Slice 2: Exporting Variables (Outputs) with Versioning & Rich Types End-to-End [DONE]
Enable writing variables of any Terraform type, storing history.
*   **Tasks:**
    *   [x] **Backend:** Extend `Store` interface for Variables. Database table `variables` must use composite PK `(namespace, name, version)`.
    *   [x] **Backend:** Implement `PutVariable` to increment version on write. Support `force_actuation` (increments nonce even if value is same). Implement `DeleteVariable`.
    *   [x] **Provider:** Implement `varlet_output` resource. Serialize TF values to `google.protobuf.Value`. Support `force_actuation` attribute.
*   **Acceptance Criteria:**
    *   Applying `varlet_output` with a map or list successfully writes it to DB.
    *   Updating the variable creates a new row with incremented version.
    *   Reading the variable returns the latest version.
*   **Blocked by:** Slice 1

### Slice 3: Consuming Variables & Dependency Protection (with DAG Enforcement) End-to-End [DONE]
Enable reading variables, tracking dependencies, and preventing cycles/unsafe deletions.
*   Tasks:
    *   [x] **Backend:** Define `Store` interface for Dependencies. Implement SQLite store. Implement `RegisterConsumer`, `DeregisterConsumer`, and `GetVariableValue` gRPC handlers. Return `actuation_nonce` in responses.
    *   [x] **Backend:** Implement **Cycle Detection** (DAG enforcement) in `RegisterConsumer`.
    *   [x] **Backend:** Update `DeleteVariable` to block if there are active consumers.
    *   [x] **Provider:** Implement `varlet_input` resource. Deserialize `google.protobuf.Value` back to TF types. Expose `trigger` (actuation nonce) attribute.
*   **Acceptance Criteria:**
    *   Stack A exports `var1`. Stack B consumes `var1`. Stack B cannot export `var2` if Stack A consumes `var2` (circular dependency blocked).
    *   Attempting to delete Stack A's variable fails if Stack B is consuming it.
*   **Blocked by:** Slice 2

### Slice 4: Namespace Metadata, Policy & Retention End-to-End [DONE]
Configure namespace metadata, access control allowlists, retention policies, and webhook URLs.
*   Tasks:
    *   [x] **Backend:** Update `namespaces` table to store `run_webhook_url` and `retention_policy` (min_versions, max_age_days).
    *   [x] **Backend:** Implement `SetNamespacePolicy` (allowlist). Enforce **Version Retention** during `PutVariable` (prune old versions).
    *   [x] **Backend:** Enforce access control allowlist (with wildcards) during reads/registrations.
    *   [x] **Provider:** Implement `varlet_namespace` **resource** to configure `allowed_consumers`, `retention_policy` block, and `run_webhook_url`.
*   **Acceptance Criteria:**
    *   Stack A configures retention (min=3, age=30d). Writing 5 versions in 1 run prunes the oldest 2 (if older than 30d, or keep 3 anyway).
    *   Access control allowlist restricts consumption as before.
*   **Blocked by:** Slice 3

### Slice 5: Dependency Graph API [DONE]
Expose the dependency graph for visualization.
*   Tasks:
    *   [x] **Backend:** Implement `GetDependencyGraph` gRPC handler. Query the `dependencies` table to construct the nodes and edges of the graph.

### Slice 6: Change Propagation (Actuation Webhooks)
Automatically trigger downstream stack runs when dependencies change.
*   **Tasks:**
    *   **Backend:** Implement webhook caller client.
    *   **Backend:** When a variable changes (regular write or forced via `force_actuation`), query `dependencies` to find all consumer namespaces.
    *   **Backend:** For each consumer, if `run_webhook_url` is configured, send an HTTP POST request to trigger the run.
*   **Acceptance Criteria:**
    *   Stack B configures `run_webhook_url` pointing to a local test HTTP server.
    *   Stack A updates a variable.
    *   Verify the test HTTP server receives a POST request.
*   **Blocked by:** Slice 4

### Slice 7: Audit Logging
Log all state-changing operations for security and debugging.
*   **Tasks:**
    *   **Backend:** Add `audit_logs` table to SQLite store.
    *   **Backend:** Implement gRPC interceptor/middleware to automatically log all write/delete/register operations, capturing the actor's identity from gRPC metadata.
*   **Acceptance Criteria:**
    *   Perform namespace policy updates, variable writes, and consumer registrations.
    *   Verify the `audit_logs` table contains detailed entries with correct timestamps, actions, targets, and actor identities.
*   **Blocked by:** Slice 6
