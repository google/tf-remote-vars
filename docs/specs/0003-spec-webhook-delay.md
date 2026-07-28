## Problem Statement

In a multi-stack infrastructure topology managed by Varlet, stacks have logical dependencies and share variables (outputs/inputs) over remote namespaces.
However, two primary issues existed in the codebase:

1. **Security Violation (Public by Default):**
   The Varlet system design specifies that namespaces should be private by default (only the owner can read variables). However, the implementation left namespaces open by default when no `allowed_consumers` were configured. This created a security risk where stacks could consume variables they weren't explicitly allowed to access.
2. **Coupled Actuation Delays:**
   Replicating execution delays (like an initial delay before a stack actuates after an upstream change) required using native Terraform delay resources (such as `time_sleep`). This forced configuration authors to couple time delay logic within their stack code, which is hard to manage and blocks the upstream stack's execution during `terraform apply`.

## Solution

1. **Private by Default Namespaces:**
   Enforce that a namespace is private by default in the backend server. If no `allowed_consumers` are configured on a namespace, access is restricted solely to the owner stack. A wildcard `"*"` pattern is supported to explicitly declare a namespace as "public" to all consumers.
2. **Asynchronous Webhook Trigger Delay:**
   Introduce a `webhook_delay_minutes` property on namespaces. When a change in an upstream variable is registered, the Varlet backend delays calling the downstream stack's webhook by the configured time. This enables background scheduling of downstream actuations without blocking the upstream writer stack.

## User Stories

1. As a platform security engineer, I want namespaces to be private by default, so that sensitive variable values cannot be read by unauthorized stacks.
2. As a platform engineer, I want to declare a namespace as public by using a wildcard (`"*"`), so that bootstrap or global settings can be easily shared with all stacks in the organization.
3. As a DevOps engineer, I want to configure a delay on a consumer stack's actuation webhook, so that its execution is delayed to allow time for upstream changes or other external services to stabilize.
4. As a developer, I want the actuation delay to be managed in the background by the Varlet server, so that my upstream stack's deployment runs immediately without blocking on a `time_sleep` wait.

## Implementation Decisions

*   **Protobuf Contract Changes:**
    Updated `RegisterNamespaceRequest` and `GetNamespaceResponse` to include `int32 webhook_delay_minutes`.
*   **Database Schema Updates:**
    Added `webhook_delay_minutes` column (INTEGER, default 0) to the SQLite `namespaces` table. Modified the query functions `RegisterNamespace` and `GetNamespace` to store and load this field.
*   **Access Control Enforcement (Server):**
    Removed the bypass check in the server's `checkAccess` function that allowed access if `allowed_consumers` was empty. Empty policies now fall through to `PermissionDenied`.
*   **Asynchronous Webhook Dispatcher (Server):**
    Modified the `propagateChange` method. For each consumer namespace, it checks if `webhook_delay_minutes > 0`. If so, the goroutine sleeps for the specified time using `s.clock.Sleep` before initiating the POST request to the webhook URL.
*   **Terraform Provider Updates:**
    Added `webhook_delay_minutes` (Int64, computed, default 0) to the `varlet_namespace` schema, models, and CRUD handlers.

## Testing Decisions

*   **Testing Seam:**
    We test at the **gRPC service interface level** (`backend.Server`) using a mock SQLite database. This tests the highest logical layer of the backend, verifying that gRPC calls correctly manipulate variables and correctly trigger webhook side-effects under mock time.
*   **Mocking Time:**
    To test the delay without introducing real time sleeps in test suites, we utilize `clockwork.FakeClock`. We trigger a change and assert that no webhook is received, advance the fake clock (`fakeClock.Advance(5 * time.Minute)`), and then assert that the webhook fires.
*   **Prior Art:**
    Tests were added directly to `backend/server_test.go`, mirroring `TestWebhookPropagation`.

## Out of Scope

*   A queue-based retry mechanism in case the webhook fails after the delay has elapsed (webhooks are fired best-effort).
*   Dynamic adjustment of delays for already scheduled/sleeping webhook calls.

## Further Notes

All existing tests were modified to set up explicit namespace policies using `SetNamespacePolicy` to comply with the new private-by-default logic.
