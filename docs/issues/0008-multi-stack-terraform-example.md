# 0008 — Multi-Stack Terraform Example

**What to build:**
Create a complete multi-stack Terraform configuration example under `examples/multi-stack-deployment/` that models a realistic 7-stack DAG layout, demonstrating both private-by-default namespaces, public namespaces, and background webhook trigger delays.

**Blocked by:** 0007 — Terraform Provider Webhook Delay Support

**Status:** ready-for-agent

- [x] Folder structure contains directories for `bootstrap`, `tagging_service`, `security_tier_1`, `security_tier_2`, `policy_engine`, `lockdown_enforcer`, and `regional_landing_zones`.
- [x] Stacks explicitly use `depends_on = [varlet_namespace.self]` to assert that their own namespace is registered before writing outputs or registering consumers.
- [x] The `bootstrap` namespace is public (`allowed_consumers = ["*"]`).
- [x] The `tagging_service` namespace is configured with `webhook_delay_minutes = 2` and a mock webhook URL.
- [x] Other namespaces restrict their `allowed_consumers` according to the DAG topology.
- [x] A `README.md` is provided at the root of the example describing the graph topology with a Mermaid diagram and mapping details.
