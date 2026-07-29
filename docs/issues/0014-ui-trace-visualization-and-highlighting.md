# 0014 — UI Trace Visualization & Highlighting

**What to build:**
Interactive lineage tracing in the UI. Make the Actuation UUIDs in the sidebar clickable to call the backend trace API and highlight participant nodes and edges on the dependency graph.

**Blocked by:**
- 0012 — Trace API Implementation
- 0013 — UI Sidebar and Node Status Details

**Status:** completed

- [x] Add a new HTTP handler `/api/trace` in `cmd/varlet-server/main.go` that forwards requests to the `GetActuationTrace` RPC and returns JSON.
- [x] Make Actuation UUIDs listed in the UI sidebar clickable.
- [x] Clicking a UUID fetches the lineage trace from `/api/trace`.
- [x] Parse the returned `TraceNode`s and `TraceEdge`s and highlight them in the Cytoscape graph (e.g., by adding highlighting CSS classes and fading out all non-participating nodes/edges).
- [x] Provide a UI control (such as a close button or pressing the Escape key) to exit the trace highlighting view and return the graph to its normal state.
