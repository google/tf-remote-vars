# 0004. Lineage and Status Details in UI

This spec outlines the design for visualizing actuation lineage, causal status details, and propagation traces in the Varlet dependency graph UI.

## Problem Statement

In a multi-stack infrastructure topology managed by Varlet, stacks have logical dependencies and share variables over remote namespaces. While Varlet tracks actuations and their parent-child relationships (lineage) in the backend database, this information is not exposed in the UI. 

Currently, users looking at the Varlet UI face the following limitations:
1. **Opaque "Affected" Status**: When a stack is marked "affected" or "potentially-affected", users cannot see *why* it is affected (i.e. which specific upstream changes or Actuation UUIDs triggered this state).
2. **Auditing Gaps**: When a stack actuates, users cannot easily see what caused the run (was it an organic run, or triggered by specific parent changes? If triggered, which parents, variables, and Actuation UUIDs were involved?).
3. **No End-to-End Tracing**: There is no way to trace a specific Actuation UUID across the dependency graph to visualize how changes propagated downstream.

## Solution

We will extend the Varlet backend and UI to provide detailed status information and trace visualization:

1. **Persistent Affected Tracking**: Store why a stack is affected in the database, linking affected stacks to their causal Actuation UUIDs.
2. **Debounce-aware Propagation**: Accumulate causal Actuation UUIDs during the debounce window and persist them when the propagation fires.
3. **Status Details API**: Update `GetDependencyGraph` to return detailed status information (causal UUIDs, active actuation, last actuation) for each namespace.
4. **Trace API**: Add a `GetActuationTrace` RPC to recursively fetch the ancestor lineage and involved variables for any given Actuation UUID.
5. **Details Sidebar**: Add a collapsible side panel to the UI. Clicking a stack displays its status, causal UUIDs, and last actuation trace.
6. **Graph Highlighting**: Clicking an Actuation UUID in the UI highlights the participant nodes/edges in the graph and fades out non-participants, allowing users to visually trace the propagation path.

## User Stories

1. As a platform operator, I want to click on an affected stack in the UI, so that I can see the list of upstream Actuation UUIDs that caused it to be affected.
2. As a developer, I want to see the specific variables that changed in an upstream actuation and are consumed by my stack, so that I know why my stack needs to be redeployed.
3. As a DevOps engineer, I want to click on a stack and see its last actuation details, so that I can quickly verify if it was triggered organically or by a webhook.
4. As an SRE, I want the affected status details and trace logs to survive server restarts, so that I don't lose audit context during server maintenance.
5. As a troubleshooter, I want to click on a specific Actuation UUID and see the full trace path highlighted in the graph, so that I can visually map out the blast radius of a change.
6. As a visual user, when tracing an actuation, I want non-participating stacks to fade out, so that I can easily focus on the active data flow path.
7. As a developer, I want trace details to load on-demand when I click a stack or UUID, so that the initial loading of the main dependency graph remains fast and responsive.

## Implementation Decisions

*   **Protobuf Changes (`proto/v1/varlet.proto`):**
    - Introduce `NamespaceStatusInfo` containing `status` (string), `causal_actuation_uuids` (repeated string), `active_actuation_uuid` (string), and `last_actuation_uuid` (string).
    - Update `GetDependencyGraphResponse`'s status map from `map<string, string>` to `map<string, NamespaceStatusInfo>`.
    - Add `GetActuationTrace(GetActuationTraceRequest) returns (GetActuationTraceResponse)` RPC.
    - Introduce `TraceNode` (containing UUID, namespace, source, status, timestamp) and `TraceEdge` (containing child UUID, parent UUID, and repeated variable names).
*   **Database Schema Updates:**
    - Create `affected_namespaces` table with columns `namespace` (TEXT, PK, FK) and `causal_actuation_uuid` (TEXT, PK, FK).
*   **Server Changes (`backend/server.go`):**
    - Update `GetDependencyGraph` to populate `NamespaceStatusInfo` by querying the `affected_namespaces` and `actuations` tables.
    - Implement `GetActuationTrace` using a SQLite recursive CTE to traverse `actuation_lineage` and collect all ancestor actuations. Filter the modified variables from the `variables` table by the consumer's `dependencies`.
    - Modify `debounceSucceeded` to collect all `ActuationUUID`s during the debounce window, and when firing, insert them into `affected_namespaces` for downstream consumers.
    - Modify `markActuating` to clear `affected_namespaces` for the starting stack.
*   **UI Changes (`cmd/varlet-server/web/index.html`):**
    - Add a collapsible right-side sidebar.
    - Handle the new `NamespaceStatusInfo` structure when parsing graph node statuses.
    - When a node is clicked, show its detailed status, causal UUIDs, and last actuation trace in the sidebar.
    - Clicking a UUID calls `GetActuationTrace` and adds CSS classes to highlight participant nodes/edges and fade out the rest of the Cytoscape graph.

## Testing Decisions

*   **Testing Seam:**
    We test at the **gRPC service interface level** (`backend.Server`) using a mock SQLite database (in-memory). This allows validating that the server logic (debounce, propagation, trace retrieval) behaves correctly without requiring a running Terraform environment or real browser testing.
*   **Trace Testing:**
    We will add tests to `backend/server_test.go` that set up a multi-level topology (`A -> B -> C`), actuate `A`, wait for propagation, and assert that `GetActuationTrace` returns the correct recursive chain of actuations, namespaces, and variable names.
*   **Affected State Persistence Testing:**
    We will write tests to assert that causal UUIDs are persisted to `affected_namespaces` on propagation and are correctly cleared when the consumer stack starts its own actuation.
*   **Prior Art:**
    Tests will mirror existing webhook propagation tests (e.g. `TestWebhookPropagation`) but with assertions on the database and gRPC responses for trace and status details.

## Out of Scope

*   Visualizing downstream "future" impact prediction for an active actuation (we only trace historical propagation/ancestors).
*   Redoing the Terraform provider to expose local trace information directly in CLI.
*   Supporting trace visualization across multiple independent Varlet servers.
