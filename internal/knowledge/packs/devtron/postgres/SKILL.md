---
name: postgres
description: The orchestrator's own database. Devtron-wide slowness almost always
  terminates here.
---

# Devtron Postgres

## Why it is in this pack
The orchestrator holds all app, pipeline, environment, RBAC and workflow-history state here. When Postgres degrades, **every** Devtron symptom appears at once: slow UI, slow API, stuck deployments, timeouts in unrelated features. An investigation that starts at the symptom will wander; one that checks Postgres early will not.

## How it fails
1. **Connection exhaustion.** `pg_stat_activity_count` at `pg_settings_max_connections`. Everything 5xxes together. Look for `idle in transaction` — that is a leak in a caller, not load.
2. **Lock contention.** A long migration or a stuck transaction blocks writers; reads look fine, writes hang.
3. **Disk full.** Workflow history and event tables grow without pruning. Postgres goes read-only and Devtron fails in confusing ways.
4. **Slow queries after growth.** App listing is usually the first thing to get slow (`app_listing_duration_seconds`).

## Remediation
- Check connections and `state` breakdown before anything else when *many* Devtron components look unhealthy at once.
- `idle in transaction` → find and fix the caller; raising max_connections hides it.
- Disk growth → prune CI/CD history with Devtron's retention settings rather than deleting rows by hand.
- A Postgres restart drops every in-flight Devtron workflow. Flag that explicitly when suggesting it.
