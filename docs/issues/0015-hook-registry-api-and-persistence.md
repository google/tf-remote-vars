# 0015 — Hook Registry API and Persistence

**What to build:**
Expose gRPC API endpoints to register and deregister global webhook callback URLs for cascade completion events, persisting them in the database to survive server restarts.

**Blocked by:** None — can start immediately

**Status:** ready-for-agent

- [ ] SQLite database migrations add the `completion_hooks` table containing `id` (INTEGER PRIMARY KEY AUTOINCREMENT), `url` (TEXT UNIQUE NOT NULL), and `created_at` (TIMESTAMP).
- [ ] Protobuf schema defines `RegisterCompletionHookRequest`, `RegisterCompletionHookResponse`, `DeregisterCompletionHookRequest`, and `DeregisterCompletionHookResponse` messages.
- [ ] Protobuf schema registers the `RegisterCompletionHook` and `DeregisterCompletionHook` RPC endpoints in the `VarletService` service.
- [ ] Protobuf files are successfully recompiled and Go code generated.
- [ ] Implement database helper methods `RegisterCompletionHook` and `DeregisterCompletionHook` in `store.go`.
- [ ] Implement RPC handlers in `backend/server.go` to handle client registration and deregistration requests, validating inputs (e.g., ensuring hook URLs are valid URLs).
- [ ] Integration tests in `backend/server_test.go` verify that hooks can be registered, deregistered, and that duplicate registrations or invalid URLs are rejected.
