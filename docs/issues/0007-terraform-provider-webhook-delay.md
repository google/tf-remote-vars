# 0007 — Terraform Provider Webhook Delay Support

**What to build:**
Expose the `webhook_delay_minutes` attribute on the `varlet_namespace` Terraform resource so configuration authors can configure it directly in HCL.

**Blocked by:** 0006 — Backend Webhook Delay Logic

**Status:** ready-for-agent

- [x] `varlet_namespace` resource schema includes `webhook_delay_minutes` (Int64, computed, default 0).
- [x] Model `NamespaceResourceModel` updated with `WebhookDelayMinutes` property.
- [x] `Create`, `Read`, and `Update` functions in `provider/resource_namespace.go` correctly map HCL state to/from gRPC protobuf messages.
- [x] Provider package tests compile and pass.
