# 6. Namespace Access Control

We decided to implement an allowlist-based access control policy for namespaces, allowing owners to restrict which other namespaces can consume their variables.

## Context

In shared environments, some variable values (like database credentials, internal IPs, or security keys) should not be visible to all stacks. Namespace owners must be able to control access to their exported variables.

## Decision

1.  **Allowlist Policy:** Each namespace can define a list of allowed consumer namespaces (or patterns).
2.  **Wildcard Support:** The allowlist will support basic wildcard matching (e.g., `myorg.myapp.*.dev` or `myorg.myapp.gce.*`) to simplify management.
3.  **Enforcement:** The backend will check the policy during `RegisterConsumer` and `GetVariableValue` requests. If the consumer namespace is not allowed, the backend will return a `PERMISSION_DENIED` gRPC status error.
4.  **Configuration via Resource:** We will introduce (or repurpose) the `varlet_namespace` resource to manage this policy. The stack that owns the namespace will manage its policy:

```hcl
resource "varlet_namespace" "self" {
  name = "myorg.myapp.network.dev" # Must match the provider's namespace
  allowed_consumers = [
    "myorg.myapp.mig.dev",
    "myorg.myapp.gce.*"
  ]
}
```

This resource will call a new `SetNamespacePolicy` gRPC RPC.
5.  **Bootstrap/Default:** If no policy is explicitly set, the default behavior could be "private" (only the owner namespace can read) or "public" (any namespace can read). We will default to **private** for safety, requiring explicit authorization.
