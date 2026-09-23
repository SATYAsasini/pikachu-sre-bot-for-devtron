---
name: chart-sync
description: Syncs Helm chart repositories into Devtron. When it stalls, new chart
  versions simply never appear in the UI.
---

# Devtron Chart Sync

Container `chart-sync`, namespace `devtroncd`, runs as a periodic job.

## How it fails
- Unreachable or slow chart repository — `repo_sync_duration_seconds` carries `error_type`.
- Credentials for a private chart repo.
- Malformed `index.yaml` in an upstream repo, which fails one repo only.

## Remediation
Chart sync is not on the deployment path: existing deployments are unaffected, only *new chart versions* are missing. State that severity clearly. Fix is usually repo reachability or credentials; read `error_type` first.
