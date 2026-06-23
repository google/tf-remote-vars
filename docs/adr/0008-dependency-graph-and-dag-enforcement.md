# 8. Dependency Graph and DAG Enforcement

We decided to expose the dependency graph via the API and enforce a Directed Acyclic Graph (DAG) relationship (no circular dependencies) on the backend.

## Context

As the number of stacks grows, the relationships between them can become complex. 
- Users need to visualize these dependencies to understand the impact of changes.
- Circular dependencies (e.g., Stack A depends on B, B depends on A) must be prevented because they make it impossible to apply or destroy stacks cleanly in Terraform.

## Decision

1.  **DAG Enforcement:** 
    *   During `RegisterConsumer`, the backend will perform cycle detection (e.g., using Depth-First Search) on the dependency graph before committing the new dependency.
    *   If a cycle is detected (e.g., registering Stack A -> Stack B would create a loop because Stack B already depends on Stack A), the operation will fail.
    *   The backend will return a `FAILED_PRECONDITION` gRPC status error with details about the detected cycle.

2.  **Graph Query RPC:**
    *   We will expose a `GetDependencyGraph` RPC that returns the nodes (namespaces) and edges (dependencies) of the graph.
    *   This API can be used by visualization tools or CLI commands to render the graph.
