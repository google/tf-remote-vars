# 2. Provider-Level Namespace Configuration

We decided to configure the stack's own namespace (the "Current Namespace") at the provider level, rather than managing it via a `varlet_namespace` resource in the same stack.

## Context

A stack needs to identify itself when exporting variables (to know where to write) and when consuming variables (to register dependencies). 

We considered managing this identity via a `varlet_namespace` resource, but referencing it in every output and input resource was verbose. Configuring it at the provider level allows resources to implicitly inherit this context.

## Decision

The `varlet` provider will accept a `namespace` argument:

```hcl
provider "varlet" {
  namespace = "myorg.myapp.mig.dev"
}
```

*   `varlet_output` resources will automatically write to this namespace.
*   `varlet_input` resources will automatically use this namespace as the `consumer_namespace` when registering dependencies.
*   The provider will ensure this namespace exists on the backend (creating it on-demand) during initialization.

To reference and validate other namespaces safely, we will provide a `varlet_namespace` **data source**:

```hcl
data "varlet_namespace" "network" {
  name = "myorg.myapp.network.dev"
}
```
