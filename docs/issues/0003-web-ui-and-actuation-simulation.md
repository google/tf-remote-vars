# 0003 — Embedded Web UI and Actuation Simulation

**What to build:**
An interactive Web UI served directly by the Varlet server that visualizes the dependency graph and allows operators to simulate the propagation of an actuation (deployment) step-by-step. The UI should color-code nodes to indicate their state during simulation (Idle, Actuating, Succeeded, Affected, Potentially Affected) so that operators can plan deployment order.

**Blocked by:** 0002 — Scoped Dependency Graph API and CLI Exporter

**Status:** completed

- [x] The `varlet-server` binary runs both a gRPC server and a parallel HTTP server.
- [x] The HTTP server exposes `GET /api/graph` returning the graph in JSON format (supporting `root` and `upstream_depth` query parameters).
- [x] The HTTP server serves an embedded single-page HTML application at `/`.
- [x] The Web UI renders the graph using Cytoscape.js with a hierarchical layout showing data flow direction.
- [x] Operators can trigger a simulation by double-clicking (or right-clicking) a node.
- [x] A "Next Step" button advances the simulation, transitioning nodes from `affected` (orange) $\rightarrow$ `actuating` (yellow) $\rightarrow$ `succeeded` (green), and propagating `affected` and `potentially-affected` (blue) states downstream.
