# 0004 — Private by Default Namespaces

**What to build:**
Enforce a "private by default" security model for Varlet namespaces. By default, only the owner namespace can read its own variables. Other namespaces must be explicitly allowed via the `allowed_consumers` configuration. A wildcard `"*"` pattern is supported to explicitly declare a namespace as public to all consumers.

**Blocked by:** 0003 — Embedded Web UI and Actuation Simulation

**Status:** completed

- [x] Empty `allowed_consumers` list in a namespace blocks other namespaces from registering as consumers or reading its variables (returns `PermissionDenied`).
- [x] Setting `allowed_consumers = ["*"]` allows any consumer namespace to read variables.
- [x] Existing backend tests in `server_test.go` that rely on cross-namespace access are updated to set explicit policies.
- [x] All backend tests compile and pass.
