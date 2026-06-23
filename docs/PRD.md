# Product Requirement Document: Varlet

## Problem Statement

Managing variable values across multiple Terraform stacks often leads to "tfvars hell." This manifests as duplicated values, difficulty sharing outputs between stacks (without resorting to complex remote state reads that expose too much data), and the risk of breaking downstream stacks by deleting or changing variables they depend on.

## Solution

We will build a Terraform provider (`varlet`) backed by a gRPC service (`varlet-backend`) that stores variable values in a remote database. 

The key differentiator is **dependency tracking**: when a stack consumes a variable, it registers itself as a consumer. The backend will block any attempts to delete variables that have active consumers.

## User Stories

1.  As a platform engineer, I want to declare a namespace for my stack, so that I can isolate and group my stack's outputs.
2.  As a platform engineer, I want to export a variable (Output) to my namespace, so that it becomes available to other stacks.
3.  As a platform engineer, I want to consume a variable (Input) from another namespace, so that I can use its value in my configuration.
4.  As a platform engineer, I want the provider to register my stack's dependency when I consume a variable, so that the owner cannot delete it and break my stack.
5.  As a platform engineer, I want to validate that a source namespace exists during the planning phase, so that I get early feedback if a dependency is missing.
6.  As a security administrator, I want to audit all changes (writes, deletes, consumption registrations) with the identity of the actor, so that I can track down changes and debug outages.
7.  As a backend developer, I want the database layer to be abstracted, so that we can support multiple databases (SQLite, PostgreSQL, Spanner) depending on the deployment environment.
8.  As a developer, I want to use SQLite for local development, so that I can test the system with zero database setup.
9.  As a platform engineer, I want to restrict which namespaces can consume my stack's variables, so that I can protect sensitive configuration.
10. As a platform engineer, I want to export complex datatypes (lists, maps, objects), so that I am not limited to string values.
11. As a platform engineer, I want variable values to be versioned, so that I can track changes over time and roll back if necessary.
12. As a platform engineer, I want to visualize the dependency graph of my stacks, so that I can understand how my infrastructure is coupled.
13. As a platform engineer, I want the backend to prevent circular dependencies, so that I don't create unresolvable stack relationships.
14. As a platform engineer, I want to define a retention policy for variable versions, so that I can control database size.
15. As a platform engineer, I want changes in parent variables to trigger runs in downstream consumer stacks (regular and forced), so that updates propagate automatically.

## Implementation Decisions

*   **VCS/Language:** The project will be built in Go (both provider and backend).
*   **API Protocol:** gRPC. The API is defined in `proto/v1/varlet.proto`.
*   **Terraform Integration:**
    *   **Provider Configuration:** Configured with the current stack's `namespace` (representing the stack's identity).
    *   **`varlet_output` (Resource):** Exports variables to the current namespace. Supports all Terraform datatypes by converting to/from JSON-compatible structures. Supports `force_actuation` attribute to force consumers to re-apply.
    *   **`varlet_input` (Resource):** Consumes variables. Exposes `value` and a `trigger` attribute (actuation nonce) to allow users to force downstream updates using `replace_triggered_by`.
    *   **`varlet_namespace` (Resource):** Manages the namespace metadata, access control policy (`allowed_consumers`), `retention_policy` (min_versions, max_age_days), and `run_webhook_url`.
    *   **`varlet_namespace` (Data Source):** Validates the existence of a source namespace.
*   **Backend Architecture:**
    *   **Repository Pattern:** A `Store` interface will abstract all database operations.
    *   **SQLite MVP:** The initial implementation will use SQLite.
    *   **Deletion Protection:** The backend will block `DeleteVariable` if the variable has active entries in the `dependencies` table, returning `FAILED_PRECONDITION`.
    *   **Access Control:** The backend will block variable reads and dependency registrations if the consumer namespace is not on the allowlist for the source namespace. Wildcards are supported.
    *   **Versioning & Retention:** The backend stores historical versions. During writes, it prunes old versions based on the namespace's `retention_policy`.
    *   **DAG Enforcement:** The backend will run cycle detection before registering a dependency, blocking any registration that creates a loop.
    *   **Dependency Graph API:** Exposes `GetDependencyGraph` to retrieve nodes and edges for visualization.
    *   **Actuation (Propagation):** The backend will call `run_webhook_url` for all consumer namespaces when a variable is updated (or forced via `force_actuation`).
    *   **Audit Logging:** All write/delete/register operations will write to an `audit_logs` table, capturing the actor's identity from gRPC metadata.

## Testing Decisions

*   We will test external behavior of the backend by running integration tests against the gRPC service with an in-memory or temporary SQLite database.
*   We will test the Terraform provider using Terraform's acceptance testing framework, spinning up a local test instance of the gRPC backend.
*   The key test scenarios will include:
    *   Exporting a variable and reading it.
    *   Registering a consumer and verifying that deletion of the variable is blocked.
    *   Deregistering the consumer and verifying that deletion is then allowed.
    *   Verifying audit log entries are created for each action.

## Out of Scope

*   Authentication/Authorization implementation (we will assume identity is passed in gRPC metadata, but actual authentication mechanism like OAuth/IAM is out of scope for MVP).
*   UI for managing variables and visualizing the dependency graph (we provide the API, but the UI itself is out of scope).
