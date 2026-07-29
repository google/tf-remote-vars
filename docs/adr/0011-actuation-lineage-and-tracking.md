# 11. Actuation Lineage and Tracking

We decided to track all actuations with unique IDs (UUIDs) and store their parent-child relationships (lineage) to allow complete auditing of how upstream changes propagate to downstream stacks.

## Context

When a stack is actuated (e.g. via `terraform apply`), it can write variables. Downstream stacks that consume these variables are then triggered (via webhooks).
We need to audit:
- Which actuation ran, when, and what variables it changed.
- If it was triggered by webhooks, which upstream parent actuations were the cause.
- Multiple upstream changes can happen close in time, meaning a single downstream run might be caused by multiple distinct parent runs.

## Decision

To support this, we introduce the following mechanism:

1. **Actuation Lineage Tracking**:
   - Every actuation gets a unique UUID.
   - We store the lineage mapping parent actuation UUIDs to child actuation UUIDs.
   - Variables are linked to the Actuation UUID that wrote them.
   - Audit logs capture the actuation runs, their statuses, and parent causes.

2. **Backend Webhook Queueing and Deduplication**:
   - Instead of firing webhooks immediately for each variable change, the backend queues pending webhooks.
   - We introduce `dedup_delay_minutes` (sliding window of quiet time) and `max_dedup_changes` (hard cap on number of combined changes) to control the deduplication.
   - When the sliding window expires (or the cap is reached), the backend:
     1. Generates a new unique **Trigger UUID**.
     2. Links the Trigger UUID to all parent Actuation UUIDs that accumulated during the delay.
     3. Sends the webhook to the consumer containing only this Trigger UUID.

3. **Provider Integration**:
   - The Varlet Terraform provider reads `VARLET_ACTUATION_UUID` and `VARLET_UPSTREAM_ACTUATION_UUIDS` from the environment.
   - If `VARLET_ACTUATION_UUID` is not set (e.g., manual local execution), the provider generates a random UUID and marks the run source as "organic".
   - The provider passes the UUID to the backend via `StartActuation` and subsequent `PutVariable`/`DeleteVariable` calls.
   - For webhook-triggered runs, the orchestrator passes the Trigger UUID received from the webhook as `VARLET_ACTUATION_UUID` to the Terraform run. Since the backend already recorded the parents of the Trigger UUID when queueing the webhook, the downstream run automatically inherits the correct lineage.
