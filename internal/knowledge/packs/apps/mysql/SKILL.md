---
name: mysql
description: Relational database. Connections, replication lag and slow queries dominate.
---

# MySQL / MariaDB

## Top failure modes
1. **Too many connections** — every client fails at once. Usually missing pooling.
2. **Replication lag or broken replication** — stale reads, or a replica that stopped entirely.
3. **Lock contention** — `innodb_row_lock_time_avg` rising; writes queue.
4. **Disk full** — binlogs grown without purging is the usual cause.

## Remediation
- Connections: pool at the application; raising `max_connections` costs memory per thread.
- Binlog growth: set `expire_logs_days` / `binlog_expire_logs_seconds`. **Deleting binlogs by hand can break replication and point-in-time recovery** — flag it.
- Replica restart may require reseeding. Say so before recommending it.
