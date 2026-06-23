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
