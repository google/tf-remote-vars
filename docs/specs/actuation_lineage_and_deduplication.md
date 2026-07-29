# Actuation Lineage Tracking and Webhook Deduplication Spec

## Problem Statement

When multiple variables in a namespace are updated close in time (e.g. during a single Terraform run that changes multiple resources), downstream stacks that consume these variables are triggered. Firing a webhook immediately for every single variable update results in redundant downstream runs (starvation/waste of resources). Furthermore, there is no way to trace which upstream actuation (and its specific parents) was responsible for triggering a downstream run.

## Solution

We introduce:
1.  **Deduplication Queue**: A database-backed webhook queue that accumulates changes for a consumer namespace over a sliding window (`dedup_delay_minutes`) and a hard limit (`max_dedup_changes`).
2.  **Actuation Lineage Tracking**: Track every actuation run with a unique UUID. Link variable versions to the actuation UUID that wrote them.
3.  **Trigger UUID & Lineage Propagation**: When the deduplication queue fires, it generates a new **Trigger UUID** that represents the combined changes, links it to all parent actuation UUIDs in the database, and passes it to the consumer webhook. The downstream runner propagates this UUID, enabling full auditing and ancestry reconstruction.

## User Stories

1. As a platform operator, I want downstream webhooks to be debounced and deduplicated, so that we save compute resources by not triggering parallel runs for every minor variable change.
2. As a downstream service owner, I want the webhook payload to contain a single unique `actuation_uuid`, so that I can trace exactly which upstream runs caused my stack to execute.
3. As an auditor, I want to query the backend database to view the parent-child lineage of any actuation, so that I can trace the root cause of a configuration rollout.
4. As a Terraform developer, I want my provider to automatically associate the variables I write with the active actuation UUID, so that I don't have to manually pass the lineage tracking IDs in my code.
5. As a platform engineer, I want to configure custom `dedup_delay_minutes` and `max_dedup_changes` on a namespace-by-namespace basis, so that we can optimize rollout speed vs resource consumption.

## Implementation Decisions

- **Database Schema**:
  - `pending_webhooks` table: Queues consumer namespaces with a `fire_at` target and a `trigger_uuid`.
  - `pending_webhook_parents` table: Maps the pending `trigger_uuid` to parent `parent_actuation_uuid`s.
  - `actuations` table: Stores details (UUID, namespace, source, status, created_at) for each actuation session.
  - `actuation_lineage` table: Links child actuation UUIDs to parent actuation UUIDs.
  - Modified `variables` table to add `actuation_uuid` column.
  - Modified `namespaces` table to add `dedup_delay_minutes` and `max_dedup_changes` columns.
- **Backend Queue Arithmetic (Sliding Window)**:
  - First change: `fire_at = now + max(webhook_delay, dedup_delay)`.
  - Subsequent changes: `fire_at = max(fire_at, now + dedup_delay)`.
  - Hard cap: If the count of accumulated parents for the trigger exceeds `max_dedup_changes`, `fire_at` is set to `now` to force immediate execution.
- **Provider Interface**:
  - Provider reads `VARLET_ACTUATION_UUID` and `VARLET_UPSTREAM_ACTUATION_UUIDS` environment variables.
  - Generates a UUID if `VARLET_ACTUATION_UUID` is missing.
  - Calls `StartActuation` RPC to signal session start, propagating parent UUIDs.
  - Attaches `actuation_uuid` to all variable `Put` and `Delete` gRPC requests.

## Testing Decisions

- **Unit Tests**:
  - Verify SQLite migration and schema integrity.
  - Verify queue logic (sliding window, max changes) via mock clocks.
- **Integration Tests**:
  - Spin up mock gRPC backend and test provider behavior under environment variables. Verify lineage is correctly created in the SQLite database.

## Out of Scope

- Visual visualization UI for the ancestry graph (ancestry can be queried via database or API, but no UI is built).
- Automatic retries for failed webhooks (the worker dispatches webhooks once, failures are logged but not automatically retried).
