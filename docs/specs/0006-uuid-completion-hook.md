# 0006 — UUID Completion Hook

## Problem Statement

In a complex multi-stack environment managed by Varlet, an organic update to a root namespace triggers a cascading series of downstream updates. Downstream stack actuations execute asynchronously—some triggered automatically via webhooks, and others applied manually by operators. 

External CI/CD orchestrators initiating the root change lack an automated, reliable mechanism to detect when the entire cascade of downstream applies is fully complete (or has failed/timed out). This makes it difficult to coordinate multi-stage release pipelines or report end-to-end deployment success.

## Solution

Introduce a completion hook registry in the Varlet backend. Operators can register global HTTP callback webhooks. A background worker tracks active organic root actuations and their downstream cascades (using parent-child actuation lineage, pending webhooks, and affected states). 

Once a root run's cascade has ended its active phase (all downstream steps are finished) or has exceeded a configurable TTL (marked as stale), the Varlet backend dispatches a JSON notification payload to all registered webhooks.

## User Stories

1. As a CI/CD orchestrator, I want to register an HTTP webhook URL in Varlet, so that I can receive end-to-end completion notifications.
2. As a CI/CD orchestrator, I want to deregister a registered webhook, so that I can stop receiving notifications when they are no longer needed.
3. As a deployment operator, I want root actuations to time out and be marked as stale after a configurable number of days, so that manual downstream steps that are never run do not block the completion webhook indefinitely.
4. As a release engineer, I want the webhook payload to specify the status of the completed run (e.g., `completed` vs. `stale`), so that I can distinguish between clean runs and runs that timed out.
5. As an infrastructure administrator, I want to query registered webhooks via Varlet's API, so that I can audit which systems are receiving completion events.

## Implementation Decisions

### 1. Hook Registry Service
*   Add a new table `completion_hooks` to store registered webhook URLs.
*   Expose gRPC RPC endpoints `RegisterCompletionHook` and `DeregisterCompletionHook` to manage the hooks.
*   Add hook configuration persistence to the SQLite backend.

### 2. Actuation Completion Tracking
*   Add `status` (active/completed/stale) and `completion_notified` (boolean flag) columns to the `actuations` table.
*   An actuation is identified as an organic root run if it has no parent entries in the `actuation_lineage` table.

### 3. Background Evaluation Worker
*   A background worker loop runs periodically (e.g., every 5 seconds).
*   **Staleness Check:** It compares the root actuation's creation time against the server-configured TTL flag (`-max-actuation-age-days`). If expired, it updates the status to `stale` and queues a notification.
*   **Completion Check:** If not stale, it recursively resolves all descendant actuations in the lineage tree. The cascade is still active if the root or any descendants are:
    1. Actively running (present in the server's in-memory actuating map).
    2. Linked as causal UUIDs in the `affected_namespaces` table.
    3. Linked as parents in `pending_webhook_parents` (waiting for deduplication to fire).
*   If none of these conditions are met, the cascade is marked `completed` and queued for notification.

### 4. Event Dispatching
*   The worker dispatches HTTP POST requests to all registered completion webhooks.
*   **Payload Format (JSON):**
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
*   Upon successful HTTP delivery, the database record is updated to `completion_notified = 1` to prevent redelivery.

## Testing Decisions

### Seams
*   gRPC API tests will verify webhook registration and deregistration logic.
*   Backend integration tests will utilize `clockwork.FakeClock` to verify timed-out (stale) transitions and trigger evaluation.
*   We will run a local test HTTP server during integration testing to intercept and verify the webhook POST payload structure and delivery.

### Key Test Scenarios
1.  **Successful Cascade Completion:** Assert that when a multi-stack deployment cascade finishes, the hook fires with `status: completed`.
2.  **Stale Cascade Timeout:** Assert that if a manual downstream step is never run, the cascade transitions to `stale` after the TTL expires and fires the webhook with `status: stale`.
3.  **No Double Delivery:** Assert that once `completion_notified = 1`, the worker never dispatches a webhook for that UUID again.

## Out of Scope

*   Granular (per-namespace or per-stack) hook registration.
*   Advanced retry backoff policies for failed hook HTTP calls (the worker will retry with its standard poll frequency until delivery succeeds).
*   Support for notifying intermediate child actuation UUID events.
