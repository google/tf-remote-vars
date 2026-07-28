# Spec: Dependency Graph Visualization and Actuation Simulation

## Problem Statement

As a platform engineer using Varlet, it is difficult to understand the complex web of dependencies between different Terraform stacks. When planning changes to a stack, it is hard to know which downstream stacks will be affected and in what order they should be redeployed (actuated) to propagate changes safely.

## Solution

Provide a dependency graph visualization tool (Web UI) and a CLI exporter. 

The Web UI allows users to:
*   Focus on a specific "root" stack.
*   Limit the view of upstream parents (providers) to reduce noise.
*   Simulate the propagation of an actuation (deployment) step-by-step to visualize the impact.

The CLI tool allows exporting the graph in DOT format for integration with standard visualization tools.

## User Stories

1.  As a platform engineer, I want to visualize the full dependency graph of all registered namespaces, so that I can see the overall coupling of my infrastructure.
2.  As a platform engineer, I want to restrict the visualization to a specific root namespace, so that I can focus on a particular stack and its relations.
3.  As a platform engineer, I want to specify how many levels of upstream parents (providers) to show for a selected root namespace, so that I can control the level of detail and avoid graph bloat.
4.  As a platform engineer, I want to see the direction of data flow (from provider to consumer) in the graph, so that I can understand how configuration changes propagate.
5.  As a platform engineer, I want to simulate the actuation (deployment) of a stack in the UI, so that I can see which downstream stacks are affected.
6.  As a platform engineer, I want the simulation to progress step-by-step, so that I can trace the exact order in which downstream stacks should be redeployed.
7.  As a platform engineer, I want the UI to color-code stacks during simulation to distinguish between:
    *   **Idle**: Unchanged stack.
    *   **Actuating**: Currently deploying.
    *   **Succeeded**: Successfully deployed.
    *   **Affected**: Needs deployment next (immediate children of actuated).
    *   **Potentially Affected**: Downstream of affected (grandchildren, etc.), may need deployment later.
8.  As a platform engineer, I want a CLI tool to query the Varlet server and output the dependency graph in DOT format, so that I can render it using Graphviz or other tools.

## Implementation Decisions

*   **API Protocol Update**: Added `upstream_depth` (int32) to `GetDependencyGraphRequest` in [varlet.proto](file:///usr/local/google/home/gkandriotti/code/tf-remote-vars/proto/v1/varlet.proto).
*   **Graph Traversal Algorithm**: Implemented in [server.go](file:///usr/local/google/home/gkandriotti/code/tf-remote-vars/backend/server.go):
    *   Find downstream descendants of the root namespace using BFS (unlimited depth).
    *   Find upstream ancestors of the root namespace using BFS (limited by `upstream_depth`).
    *   Filter nodes and edges to include only the union of these sets.
*   **Server Component**: Created [cmd/varlet-server/main.go](file:///usr/local/google/home/gkandriotti/code/tf-remote-vars/cmd/varlet-server/main.go) which runs both gRPC server (for provider) and HTTP server (for UI and JSON API) in a single binary.
*   **Web UI**: Created an embedded single-page app in [cmd/varlet-server/web/index.html](file:///usr/local/google/home/gkandriotti/code/tf-remote-vars/cmd/varlet-server/web/index.html) using Cytoscape.js and Dagre layout for graph rendering, served by the Go backend.
*   **CLI Component**: Created [cmd/varlet-cli/main.go](file:///usr/local/google/home/gkandriotti/code/tf-remote-vars/cmd/varlet-cli/main.go) that connects to the gRPC server and prints the graph in DOT format to stdout.

## Testing Decisions

*   **Seam**: We test at the **Go API level** of the `Server` struct. This allows testing the graph traversal, filtering, and cycle detection logic in memory without network overhead.
*   **Test Cases**: In [server_test.go](file:///usr/local/google/home/gkandriotti/code/tf-remote-vars/backend/server_test.go) (`TestGetDependencyGraph`), we verify:
    *   Entire graph (no root).
    *   Root with 0 parents (only descendants).
    *   Root with N parents.
    *   Root with unlimited parents (-1).
    *   Isolated nodes.
    *   Error handling for non-existent root.

## Out of Scope

*   Real-time tracking of actual deployments in the UI (only simulation is supported; real-time integration with CI/CD pipelines to report actual status is deferred).
*   Editing variables or namespaces from the Web UI (the UI is read-only).

---
**Triage Labels**: `ready-for-agent`
