# 9. Version Retention Policy

We decided to support user-configurable version retention policies at the namespace level to cap the number of stored variable versions.

## Context

Storing infinite history can lead to database bloat. Users need a way to define retention rules (e.g., "keep at least 3 versions, but prune versions older than 30 days if we have more than 3").

## Decision

1.  **Policy Definition:** Namespaces will support a `retention_policy` configuration:
    *   `min_versions` (integer) - The minimum number of versions to keep, regardless of age.
    *   `max_age_days` (integer) - Prune versions older than this many days, but only if we still keep `min_versions`.
2.  **Storage:** These settings will be stored in the `namespaces` table (or a related policy table).
3.  **Enforcement:** The backend will run the retention cleanup during `PutVariable` writes. After inserting a new version, it will query older versions and delete those that violate the policy.
4.  **Configuration:** The policy will be configured via the `varlet_namespace` resource:

```hcl
resource "varlet_namespace" "self" {
  name = "myorg.myapp.network.dev"
  
  retention_policy {
    min_versions = 3
    max_age_days = 30
  }
}
```
