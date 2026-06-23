# 5. Abstract Database Access

We decided to abstract the database operations behind a Go interface (Repository Pattern) to support multiple database backends (e.g., SQLite, PostgreSQL, Cloud Spanner).

## Context

Different users will have different hosting requirements. A local developer might want a zero-config SQLite database, while a production deployment might require PostgreSQL or Cloud Spanner for scalability and reliability. 

To avoid locking the backend code into a specific database driver or dialect, we need a clean abstraction layer.

## Decision

1.  **Define a `Store` Interface:** We will define a Go interface that declares all data access methods (namespaces, variables, dependencies, audit logs).
2.  **Transactions:** The interface will support transactional operations to ensure consistency (e.g., checking for active consumers and deleting a variable must happen atomically).
3.  **Initial Implementation:** We will implement `SqliteStore` using SQLite for the initial MVP.
4.  **Future Expandability:** Adding a new database (like PostgreSQL) will only require implementing the `Store` interface, without changing the gRPC service logic.
