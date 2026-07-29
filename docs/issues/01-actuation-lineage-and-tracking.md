# 01 — Actuation Lineage Tracking

**What to build:**
Enable tracking of Terraform actuation runs and their parent-child ancestry. Every actuation run must be assigned a unique UUID. The Terraform provider must read this UUID (and any upstream parent UUIDs) from the environment and send it to the Varlet backend. The backend must persist these actuations, record their parent-child lineage in the database, and link written variable versions to the active actuation UUID.

**Blocked by:** None

**Status:** ready-for-agent

- [ ] SQLite database migrations add the `actuations` and `actuation_lineage` tables, and add the `actuation_uuid` column to the `variables` table.
- [ ] The `StartActuation` RPC accepts `actuation_uuid` and `parent_actuation_uuids`, registering the actuation and its parent relationships in the database.
- [ ] The `PutVariable` RPC associates stored variable versions with the active actuation UUID.
- [ ] The Terraform Provider extracts `VARLET_ACTUATION_UUID` and `VARLET_UPSTREAM_ACTUATION_UUIDS` from the environment, sends them during provider configuration (`StartActuation`), and links written variables to the actuation UUID.
- [ ] Provider acceptance test `TestAccActuationLineagePropagation` successfully runs and asserts database lineage records match environment inputs.
