# 10. Actuation and Change Propagation

We decided to support change propagation (actuation) to ensure consumer stacks are re-applied when parent variables change, including support for forced actuation.

## Context

When a variable value changes, downstream stacks (consumers) must be re-applied to pick up the change. 
- **Regular Actuation:** Triggered when the variable value actually changes.
- **Forced Actuation:** Triggered when the producer wants to force all consumers to re-apply, even if the variable value remains the same (e.g., to force a redeployment of downstream resources).

## Decision

To support this, we propose a two-part mechanism:

1.  **Forced Change Detection (Nonces):**
    *   The backend will maintain an `actuation_nonce` (timestamp or counter) for each variable version.
    *   `PutVariableRequest` will include a `force_actuation` boolean. If true, the backend will increment the nonce even if the value is identical.
    *   The `varlet_input` resource will read this nonce and expose it as a `trigger` attribute. Users can use Terraform's `replace_triggered_by` lifecycle block to force resource recreation when the trigger changes:
        ```hcl
        resource "google_compute_instance" "default" {
          # ...
          lifecycle {
            replace_triggered_by = [varlet_input.vpc_id]
          }
        }
        ```

2.  **Run Triggering (Active Propagation):**
    *   *Pending Clarification:* To automatically start runs of consumer stacks, the backend needs a integration point. We propose allowing namespaces to register a `run_webhook_url` (e.g., triggering a Cloud Build or GitHub Actions run).
    *   When a variable is updated (and propagates a change or force event), the backend will identify all consumer namespaces, retrieve their webhooks, and send HTTP POST requests to trigger runs.
