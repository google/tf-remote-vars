# 13. UUID Completion Hook

We decided to implement a UUID completion hook system. This allows external orchestrators to register webhook callbacks to be notified when the cascade of downstream changes triggered by an organic root Actuation UUID is completely finished.

## Context

When an organic actuation is triggered (e.g., via a manual user change in a root stack), it creates a root Actuation UUID. This run updates variables, causing Varlet to propagate changes downstream, marking consumer stacks as `Affected` and queueing webhooks. 

Those webhooks trigger further downstream runs, establishing parent-child links in `actuation_lineage`. This cascade continues transitively down the dependency graph until it reaches terminal leaf stacks.

External orchestrators triggering the initial change need to know when the *entire* propagation cascade is finished (i.e., when all downstream stacks have finished deploying the changes). 

However, since some downstream stacks may not have automated webhooks configured (requiring manual runs) or runs may fail/be abandoned, the cascade can get stuck in an active state indefinitely. We need a way to track the active status of the cascade, notify upon completion, and handle stalled cascades cleanly via timeouts.

## Decision

To support cascade tracing and completion callbacks, we introduce the following mechanism:

### 1. Webhook Registration API
We expose gRPC endpoints to allow external clients to register webhooks globally. Webhooks are stored in the database to survive server restarts.

**New Database Table (`completion_hooks`):**
```sql
CREATE TABLE IF NOT EXISTS completion_hooks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**New gRPC RPCs:**
```proto
rpc RegisterCompletionHook(RegisterCompletionHookRequest) returns (RegisterCompletionHookResponse);
rpc DeregisterCompletionHook(DeregisterCompletionHookRequest) returns (DeregisterCompletionHookResponse);
```

---

### 2. Tracking Cascade Status in Database
To determine if an organic root run is completed and prevent firing duplicate webhooks, we update the `actuations` table:

```sql
-- Migration adds these columns to the `actuations` table:
ALTER TABLE actuations ADD COLUMN status TEXT DEFAULT 'active'; -- 'active', 'completed', 'stale'
ALTER TABLE actuations ADD COLUMN completion_notified INTEGER DEFAULT 0; -- 0 or 1
```

---

### 3. Evaluating Cascade Completion (Background Worker)
A background worker loop polls the database periodically (e.g., every 5 seconds) to identify organic root actuations that are still `active` and have not been notified (`completion_notified = 0`).

An actuation `A` is organic/root if it has no parent entries in `actuation_lineage`.

For each active organic root actuation `A`, the worker checks two conditions:

#### A. Staleness (Timeout)
If `now > A.created_at + max_actuation_age_days` (default `3` days, configured via server flag `-max-actuation-age-days`):
1. Transition `A` status to `'stale'`.
2. Add `A` to the notification batch.

#### B. Successful Completion
If the actuation is not stale, the worker queries its entire downstream cascade:
1. It runs a recursive CTE on `actuation_lineage` to find all descendant Actuation UUIDs spawned from root `A` (let's call this set `Descendants`).
2. The cascade is **still active** if any UUID in `Descendants` (including `A` itself) is:
   - Currently active in the server's memory (`s.actuating` map).
   - Linked as a `causal_actuation_uuid` in the `affected_namespaces` table (meaning a namespace is affected by it but hasn't actuated yet).
   - Linked as a `parent_actuation_uuid` in the `pending_webhook_parents` table (meaning a webhook trigger is queued but hasn't fired yet).
3. If none of these conditions are true, the cascade is complete:
   - Transition `A` status to `'completed'`.
   - Add `A` to the notification batch.

---

### 4. Hook Notification Dispatch
When organic root actuations are promoted to `completed` or `stale`, the worker dispatches an HTTP `POST` request to all URLs in `completion_hooks`.

**Payload Format (JSON):**
```json
{
  "completed_actuations": [
    {
      "uuid": "8e3c4568-d0df-4f45-bb90-e593c66f50b2",
      "status": "completed"
    },
    {
      "uuid": "14f24cda-6789-40ee-b4c6-2c949826a7e0",
      "status": "stale"
    }
  ]
}
```

Once webhooks succeed, the worker updates `completion_notified = 1` in the database to prevent repeat deliveries.
