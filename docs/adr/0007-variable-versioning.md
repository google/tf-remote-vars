# 7. Variable Versioning

We decided to store variable history by versioning variables in the database, allowing rollback and auditing of values over time.

## Context

Infrastructure configuration changes frequently. To support rollback (restoring a previous configuration) and debugging (identifying when a bad value was introduced), we need to keep a history of variable values.

## Decision

1.  **Version Column:** The `variables` table will include a `version` column. The Primary Key will be `(namespace, name, version)`.
2.  **Incremental Writes:** A `PutVariable` request will not overwrite the existing value. Instead, the backend will query the current maximum version for `(namespace, name)`, increment it by 1, and insert a new row.
3.  **Latest by Default:** By default, readers (`GetVariableValue` and `RegisterConsumer`) will receive the latest version.
4.  **Rollback:** To roll back, the producer stack can publish a new version that contains the old value. This preserves history and ensures that "forward-only" audit logs are maintained.
5.  **Future Pinning:** While the initial implementation assumes consumers always read the latest version, this structure allows us to support version pinning in the future (e.g., `varlet_input` requesting a specific version) without schema changes.
