# 3. Enforce Dependency Protection on Deletion

We decided to block the deletion of variables that have active consumers to prevent breaking downstream stacks.

## Context

One of the primary goals of tracking variable consumption is to prevent "tfvars hell" consequences, such as deleting a variable (like a VPC ID) that other stacks still rely on.

## Decision

The backend will enforce the following rules:

1.  **Block Deletion:** A `DeleteVariable` request will fail if there are one or more registered consumers (dependencies) for that variable.
2.  **Error Code:** The backend will return a `FAILED_PRECONDITION` gRPC status error code, including details about which consumer namespaces are blocking the deletion.
3.  **Force Deletion:** Although a `force` flag is present in the `DeleteVariableRequest` API for future-proofing, the initial implementation will ignore it or still block deletion to ensure safety. Overriding this block will require manual intervention or a future policy update.
