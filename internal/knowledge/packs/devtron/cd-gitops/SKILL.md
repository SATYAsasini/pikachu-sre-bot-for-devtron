---
name: cd-gitops
description: Devtron's deployment path. The orchestrator commits manifests to git
  and ArgoCD syncs them; failures land between the two and belong to neither alone.
---

# Devtron CD / GitOps

## The shape of the system
A Devtron deployment is two handoffs:

```
orchestrator --commit--> git repo --sync--> ArgoCD --apply--> cluster
     ^                                                          |
     +------------- status via kubewatch/NATS ------------------+
```

Most "deployment stuck" incidents are a break at one of the arrows, and the arrow tells you the owner.

## Diagnosing by symptom

| Symptom | Where it broke | Evidence |
|---|---|---|
| Deploy triggered, nothing in git | orchestrator GitOps | `git_ops_duration_seconds{operationName="CommitAndPushAllChanges",status!="Success"}` |
| Git has the commit, ArgoCD OutOfSync | ArgoCD sync | `argocd_app_info{sync_status="OutOfSync"}`, sync phase Failed |
| ArgoCD Synced, workload unhealthy | the application | `argocd_app_info{health_status="Degraded"}` then the workload's own events |
| ArgoCD Healthy, Devtron UI still "Progressing" | status propagation | kubewatch / NATS, and `deployment_status_cron_process_time` |
| Everything queued, nothing moves | application controller saturated | `workqueue_depth`, `argocd_app_reconcile` p99 |

That last row matters: **ArgoCD Healthy plus a stale Devtron UI is not a deployment problem at all.** It is an event-delivery problem, and restarting the app or re-deploying will not fix it.

## Common causes
- **OutOfSync that will not converge:** an immutable field changed (Deployment selector, StatefulSet volumeClaimTemplates, Job spec). Sync keeps failing with the same error; the fix requires deleting and recreating the resource, which is destructive and must be flagged.
- **Sync succeeds, health Degraded:** the manifests applied but the workload cannot start — image pull, missing config, failing probe. Stop looking at ArgoCD.
- **Hook / pre-sync job failure:** blocks the whole sync; the job's logs hold the reason.
- **Repo server unreachable or slow:** affects every app at once.
- **Controller saturation:** many apps, one shard. `workqueue_depth` sustained above zero.

## Remediation
- Always name which arrow broke before proposing an action.
- Prefer a targeted re-sync of one app over a controller restart.
- Immutable-field conflicts: state plainly that recreating the resource causes downtime, give the verification step, and let a human decide.
- Sharding or raising application-controller resources is the fix for saturation; re-syncing individual apps is not.
