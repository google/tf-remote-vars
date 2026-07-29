# 0011 — Status Details Schema & Affected Tracking

**What to build:**
Store namespace affected state (causal Actuation UUIDs) in the database and expose detailed namespace status via the `GetDependencyGraph` API. Specifically, create the `affected_namespaces` table in the database, define the structured `NamespaceStatusInfo` protobuf message, propagate causal Actuation UUIDs to the table during webhook debounce, clear them when actuation starts, and update the graph API to populate this status info. Additionally, update the UI's graph parsing to handle the new status payload structure without rendering issues.

**Blocked by:** 0010 — Webhook Deduplication Queue

**Status:** completed

- [x] SQLite database migrations add the `affected_namespaces` table (with columns `namespace` and `causal_actuation_uuid`).
- [x] Protobuf schema defines `NamespaceStatusInfo` (containing status, causal_actuation_uuids, active_actuation_uuid, last_actuation_uuid) and updates `GetDependencyGraphResponse`'s status map to `map<string, NamespaceStatusInfo>`. Also defines `GetActuationTrace` request/response messages and RPC in the protobuf.
- [x] The `debounceSucceeded` worker function collects all `ActuationUUID`s during the debounce window and inserts them into `affected_namespaces` for downstream consumers when triggering propagation.
- [x] The `markActuating` function clears records in `affected_namespaces` for the starting namespace.
- [x] The `GetDependencyGraph` RPC handler retrieves and returns detailed status information by querying `affected_namespaces` and `actuations`.
- [x] The UI (`cmd/varlet-server/web/index.html`) correctly parses the new `NamespaceStatusInfo` structure to determine the graph node classes.
- [x] Integration tests in `backend/server_test.go` verify that causal UUIDs are persisted on propagation, cleared on actuation, and returned correctly in the graph response.
