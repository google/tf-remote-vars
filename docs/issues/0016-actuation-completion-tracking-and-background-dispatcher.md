# 0016 — Actuation Completion Tracking and Background Dispatcher

**What to build:**
A background worker that periodically checks if organic root actuations have completed their downstream cascades (or timed out as stale). When completed or stale, the worker dispatches JSON POST payloads to all registered completion webhooks and updates the DB to mark them as notified.

**Blocked by:** 0015 — Hook Registry API and Persistence

**Status:** ready-for-agent

- [ ] SQLite database migrations update the `actuations` table schema to add `status` (TEXT DEFAULT 'active') and `completion_notified` (INTEGER DEFAULT 0) columns.
- [ ] Implement `GetCausalActuationUUIDs` (or a recursive helper) that evaluates if a root UUID is completed. An actuation cascade is complete if the root and all its descendant actuations in `actuation_lineage` are not in the active `actuating` map, not present in `affected_namespaces`, and not present in `pending_webhook_parents`.
- [ ] Add a global server flag `-max-actuation-age-days` (default 3).
- [ ] Implement a background worker thread that polls every 5 seconds for root actuations where `completion_notified = 0`.
- [ ] The worker marks active runs as `stale` if their age exceeds `-max-actuation-age-days`, and as `completed` if the cascade completes successfully.
- [ ] The worker dispatches HTTP POST requests with the JSON payload format `{"completed_actuations": [{"uuid": "...", "status": "completed|stale"}]}` to all URLs in `completion_hooks`.
- [ ] The worker updates `completion_notified = 1` in the database for successfully notified actuations.
- [ ] Integration tests verify that completing a multi-stage cascade triggers the callback HTTP POST, that stale cascades time out and notify correctly, and that duplicate notifications do not fire.
