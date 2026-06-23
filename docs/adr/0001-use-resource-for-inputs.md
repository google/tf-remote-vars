# 1. Use Resource (varlet_input) instead of Data Source for Inputs

We decided to use a Terraform resource (`varlet_input`) instead of a data source to consume variables from the remote database.

This is because we need to track consumers of variables on the backend. By using a resource, the creation and deletion of the resource in Terraform corresponds to register and deregister operations on the backend. This allows the backend to track dependencies and prevent breaking changes (e.g., deleting a variable that is still active in a consuming stack).
