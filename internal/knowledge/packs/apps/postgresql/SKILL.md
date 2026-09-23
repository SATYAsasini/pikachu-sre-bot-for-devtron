---
name: postgresql
description: Relational database. Failures are usually connections, disk or locks
  — rarely the process itself.
---

# PostgreSQL

## Top failure modes
1. **Connection exhaustion** — `pg_stat_activity_count` at `pg_settings_max_connections`. Everything using the DB fails together. Check the `state` split: `idle in transaction` means a client leaks transactions; use a pooler (PgBouncer) rather than raising the limit.
2. **Disk full** — Postgres goes read-only; writes fail with confusing errors. Check PVC usage, WAL growth and whether archiving is stuck.
3. **Lock contention** — reads fine, writes hang. Long-running transaction or a migration.
4. **OOMKill** — `work_mem` × concurrency exceeds the pod limit under a heavy query.
5. **Replication lag** — replicas fall behind; read-your-writes breaks.

## Remediation
- Connections: fix the leaking caller or add pooling. Raising `max_connections` increases memory per backend and often makes it worse.
- Disk: expand the PVC; check WAL archiving before deleting anything. **Deleting WAL is destructive and can break recovery — never suggest it as a first step.**
- Locks: identify the blocking PID, then decide with a human whether to terminate it.
- Restarting a primary causes connection loss and possible failover. Always state that.
