# 0013 — UI Sidebar and Node Status Details

**What to build:**
A collapsible sidebar in the Varlet dependency graph UI to display detailed status information for selected namespace nodes.

**Blocked by:** 0011 — Status Details Schema & Affected Tracking

**Status:** completed

- [x] Add a collapsible right sidebar to the UI (`cmd/varlet-server/web/index.html`).
- [x] Bind Cytoscape graph node click events to open the sidebar.
- [x] Extract detailed status information (from `NamespaceStatusInfo`) for the selected node and display:
    *   Node name and current status.
    *   The list of causal Actuation UUIDs that triggered its affected state.
    *   The last actuation UUID that ran for the namespace.
- [x] Show empty/placeholder UI states if there are no causal UUIDs or no previous actuations.
