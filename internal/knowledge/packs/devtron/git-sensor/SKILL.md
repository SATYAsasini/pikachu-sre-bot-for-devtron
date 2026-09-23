---
name: git-sensor
description: Polls and clones git repositories, detects new commits and feeds material
  updates into CI. When it stalls, builds simply never trigger.
---

# Devtron Git Sensor

## What it does
Clones and polls every git material configured in Devtron, detects new commits and notifies the orchestrator so CI can trigger. Container `git-sensor`, Service `git-sensor-service`, namespace `devtroncd`. It keeps clones on a PersistentVolume.

## How it fails
1. **Disk full on the clone PVC.** The most common failure by a wide margin. Clones accumulate; once the volume is full every git operation fails at once and `git_operation_duration_seconds{status="Failed"}` jumps across all methods simultaneously. A failure that hits *every* repo at the same moment is disk, not git.
2. **Fetch timeouts.** `git_fetch_timeout` rising, usually on one large repo, or after the git host slows down. Builds stop triggering for that repo only.
3. **Credential expiry.** Failures concentrated on one method (`fetch`) with a stable disk and a specific repo — check the git credential or deploy key.
4. **No commits detected.** `total_material_update` flat while pushes are happening: polling has stalled or webhooks are not arriving. Confirm `active_git_repo_count` still includes the repo.

## Remediation
- **Every repo failing at once** → check the PVC's free space first (`kubelet_volume_stats_available_bytes` for the git-sensor claim). Expanding the volume or pruning clones fixes it; restarting does not.
- **One repo failing** → credential or repo size. Raise the fetch timeout for large monorepos.
- **Builds not triggering, git healthy** → the problem is downstream: check that `total_material_update` is rising and then whether the CI-trigger event reached NATS.
- Note for the reader: git-sensor problems present to users as "CI is broken", so state clearly when the real fault is git access rather than the build.
