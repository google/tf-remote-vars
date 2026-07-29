# 0002 — Scoped Dependency Graph API and CLI Exporter

**What to build:** 
The ability for operators to retrieve a filtered subset of the dependency graph centered around a "root" namespace and limited to a specific number of upstream parent levels. Additionally, a new CLI tool `varlet-cli` must be available to connect to the Varlet server via gRPC, query this graph, and output it in Graphviz DOT format to stdout for easy visualization in terminal-based workflows.

**Blocked by:** None

**Status:** completed

- [x] The `GetDependencyGraphRequest` proto message supports `upstream_depth` (int32) parameter.
- [x] The backend `GetDependencyGraph` implementation correctly traverses downstream descendants (unlimited) and upstream ancestors (limited by `upstream_depth`).
- [x] A CLI tool `varlet-cli` is implemented and can be compiled.
- [x] Running `varlet-cli --server=localhost:8080 --root=A --upstream-depth=1` prints a valid DOT file to stdout representing the filtered graph.
- [x] Edges in the DOT file represent data flow direction (`source -> consumer`).
