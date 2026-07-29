# 0012 — Trace API Implementation

**What to build:**
A backend RPC endpoint `GetActuationTrace` that retrieves the recursive ancestor lineage and filtered modified variables for a given Actuation UUID.

**Blocked by:** 0011 — Status Details Schema & Affected Tracking

**Status:** ready-for-agent

- [ ] Implement the `GetActuationTrace` RPC handler in `backend/server.go`.
- [ ] The RPC handler queries the database using a SQLite recursive CTE to traverse `actuation_lineage` and gather all ancestor actuations.
- [ ] The RPC handler retrieves modified variables for the trace nodes and filters them to include only variables that are in the downstream consumer's dependencies list.
- [ ] Integration tests in `backend/server_test.go` assert that `GetActuationTrace` returns the correct recursive chain of actuations, namespaces, and variable names for a multi-stage topology.
