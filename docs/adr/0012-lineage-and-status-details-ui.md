# 12. Lineage and Status Details in UI

We decided to extend the SQLite schema and add a new `GetActuationTrace` RPC to expose and display actuation lineage and status details in the UI.

## Context

Now that we have actuation lineage tracking implemented in the backend and Terraform provider, we want to visualize this in the dependency graph UI. Specifically, users should be able to:
1. Click on a stack (namespace) in the UI and know why it is "affected" or "potentially-affected" (i.e. which upstream Actuation UUIDs caused it).
2. See what triggered a stack's last actuation (e.g. which parent actuations/variables caused it, or if it was an organic, non-varlet actuation).
3. Visualize the end-to-end trace of a particular Actuation UUID, showing how changes propagated downstream while hiding or fading non-participating nodes.

## Decision

To support these requirements while keeping the architecture simple and the main graph loading fast, we decided on the following:

1. **Database Schema Extension**:
   We will add an `affected_namespaces` table to SQLite:
   ```sql
   CREATE TABLE IF NOT EXISTS affected_namespaces (
       namespace TEXT NOT NULL,
       causal_actuation_uuid TEXT NOT NULL,
       PRIMARY KEY (namespace, causal_actuation_uuid),
       FOREIGN KEY (namespace) REFERENCES namespaces(name) ON DELETE CASCADE,
       FOREIGN KEY (causal_actuation_uuid) REFERENCES actuations(uuid) ON DELETE CASCADE
   );
   ```
   This ensures that the "affected" status details survive server restarts.

2. **In-Memory Accumulation during Debounce**:
   Causal Actuation UUIDs will be accumulated in the in-memory `debounceState` during the short debounce window (2 seconds). Once the debounce timer fires, the accumulated causal UUIDs are written to the database for all affected downstream consumers.

3. **API Changes**:
   - **`GetDependencyGraph` Response**: We will replace the plain string status map in `GetDependencyGraphResponse` with a structured `NamespaceStatusInfo` message containing the status, causal UUIDs, current active actuation, and last completed actuation.
   - **New `GetActuationTrace(actuation_uuid)` RPC**: We will introduce a new RPC that recursively resolves the parent-child lineage of a given actuation using a recursive CTE in SQLite, returning the nodes, edges, and modified variables involved in the trace.

4. **UI Design (Sidebar & Highlighting)**:
   - We will add a collapsible side panel (sidebar) to the right of the graph.
   - Clicking a node displays its status, causal UUIDs, and last actuation info in the sidebar.
   - Clicking a causal Actuation UUID calls `GetActuationTrace` and highlights only the participating nodes and edges on the graph (fading out non-participant elements) and displays the sequential trace step-by-step in the sidebar.
