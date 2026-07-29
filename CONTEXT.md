# Varlet

Addressing "tfvars hell" by storing variable values in a remote database and making them available via a Terraform provider.

## Language

**Stack**:
A Terraform deployable unit. Maps 1-to-1 with a Terraform State and a Namespace.

**Namespace**:
A unique string identifier that groups variables exported by a single Stack. No two stacks can share the same namespace.
*   **Current Namespace**: The namespace owned by the active stack, configured at the provider level.
*   **Source Namespace**: A namespace owned by another stack, from which variables are consumed. Can be referenced/validated via a data source.

**Variable**:
A key-value pair stored within a namespace.
*   **Output (Exported Variable)**: A variable published by a stack to its **Current Namespace**.
*   **Input (Consumed Variable)**: A variable read by a stack from a **Source Namespace**. Reading a variable registers a dependency on the backend, tracking the consuming stack.

**Namespace Policy**:
An allowlist of consumer namespaces (or patterns with wildcards) allowed to consume variables from a specific namespace. By default, namespaces are private (only the owner can read).

**Variable Version**:
A historical record of a variable's value. Every update to a variable creates a new version, preserving history.

**Dependency Graph**:
The directed graph representing relationships between stacks. Nodes represent namespaces, and directed edges represent variables consumed by one namespace from another.

**DAG (Directed Acyclic Graph)**:
The required state of the Dependency Graph. The backend enforces that no cycles (loops) can be created (e.g., A depends on B, and B depends on A).

**Actuation**:
The process of applying changes to a Stack (typically via `terraform apply`). Actuating a stack may update its exported variables, which can affect downstream consumer stacks. Every actuation is tracked by a unique **Actuation UUID**.

**Actuation Source**:
*   **Organic**: An actuation triggered by a user (e.g. CLI run, breakglass) or a direct code change.
*   **Webhook-triggered**: An actuation automatically triggered by Varlet because upstream parent stacks had changes.

**Actuation Lineage**:
The audit trail of parent-child relationships between actuations. A webhook-triggered actuation will have one or more parent Actuation UUIDs that caused the trigger.

**Completion Hook**:
A global registry of webhook endpoints notified when the cascade triggered by an organic root actuation completely finishes (i.e., all downstream runs are completed, and no more changes are pending or active).

**Stale/Defunct Actuation**:
An organic actuation whose cascade has exceeded `max_actuation_age_days` (default 3) and is transitioned to a stale/expired state, triggering completion callbacks to prevent deadlocks from manual downstream steps that were never run.
