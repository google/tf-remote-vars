# 0005 — Webhook Delay Proto and Storage Schema

**What to build:**
Extend the Varlet gRPC API contract and SQLite database schema to support the new `webhook_delay_minutes` namespace property.

**Blocked by:** 0004 — Private by Default Namespaces

**Status:** completed

- [x] `RegisterNamespaceRequest` (proto) supports `webhook_delay_minutes` (int32).
- [x] `GetNamespaceResponse` (proto) supports `webhook_delay_minutes` (int32).
- [x] Protobuf is recompiled and Go generated files are updated.
- [x] SQLite `namespaces` table contains `webhook_delay_minutes` column (INTEGER, default 0).
- [x] `RegisterNamespace` and `GetNamespace` store functions read/write the delay to the database.
