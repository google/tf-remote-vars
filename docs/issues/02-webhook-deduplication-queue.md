# 02 — Webhook Deduplication Queue

**What to build:**
A database-backed deduplication queue for outgoing webhooks. Instead of immediately triggering downstream consumer webhooks for every variable write, the backend must queue pending webhooks and delay them using a sliding window quiet-time (`dedup_delay_minutes`) and a hard change cap (`max_dedup_changes`). When the delay expires or the cap is reached, the worker must trigger the consumer webhook with a new generated Trigger UUID, mapping all accumulated parent actuation UUIDs to it in the database.

**Blocked by:** 01 — Actuation Lineage Tracking

**Status:** ready-for-agent

- [ ] SQLite database migrations add the `pending_webhooks` and `pending_webhook_parents` tables, and add deduplication configuration fields (`dedup_delay_minutes`, `max_dedup_changes`) to the `namespaces` table.
- [ ] RPC handlers expose and validate the new deduplication configurations when registering/retrieving namespaces.
- [ ] Variable writes queue webhook triggers in the database, calculating sliding-window `fire_at` targets and appending parent actuation UUIDs.
- [ ] A background worker loop polls for pending webhooks to fire, promotes them to "triggered" actuations in the database (recording accumulated parent lineages), and dispatches webhooks with the trigger UUID.
- [ ] Existing server tests are updated to expect the simplified webhook payload.
- [ ] Server test `TestWebhookDeduplicationAndMaxChanges` verifies sliding-window accumulation, early-fire limits, and correct parent lineage mapping in the database.
