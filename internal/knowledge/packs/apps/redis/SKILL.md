---
name: redis
description: In-memory store. Nearly every Redis incident is memory, eviction or persistence.
---

# Redis

## Top failure modes
1. **Memory pressure + eviction** — `redis_evicted_keys_total` rising. The *symptom* appears in the application (latency, cache misses), not in Redis, which stays "healthy".
2. **OOMKilled** — no `maxmemory` set, so the pod limit kills it instead of Redis evicting. This is a misconfiguration, not a capacity problem.
3. **maxclients** — `redis_rejected_connections_total` rising; usually a client that does not pool.
4. **Failed BGSAVE** — `redis_rdb_last_bgsave_status` 0. Persistence is broken, data loss on restart, and nothing alerts on it by default.
5. **Blocking commands** — `KEYS` or a large `SCAN` in production stalls the single thread.

## Remediation
- Always check whether `maxmemory` and an eviction policy are configured. Unset `maxmemory` in a container is the root cause behind most Redis OOMKills.
- Eviction with a correctly sized cache is normal; eviction plus a collapsing hit rate means resize.
- A restart clears all non-persisted data. Say that explicitly.
