---
name: orchestrator
description: The Devtron control plane API. Owns CI/CD pipelines, app and environment
  metadata, RBAC and the Kubernetes proxy every other client goes through.
---

# Devtron Orchestrator

## What it does
The control plane. Serves `/orchestrator/*`, owns app/environment/pipeline metadata in Postgres, performs GitOps commits for deployments, publishes and consumes NATS events, and proxies Kubernetes traffic for every cluster Devtron manages. Container name `orchestrator`, Service `orchestrator-devtroncd-service`, namespace `devtroncd`.

## How it fails, in order of likelihood
1. **Postgres pressure.** Slow or failing queries surface as rising `orchestrator_http_duration_seconds` and a climbing `orchestrator_http_requests_current`, often first on app listing. Check Postgres connections and slow queries before blaming the orchestrator.
2. **NATS disconnect.** If `NatsOrchestratorConsumerConnectionBreaks` fires, the orchestrator stops consuming CI/CD completion events. Symptom: builds and deployments finish in the cluster but the UI never updates. The workload is *healthy* — this is the classic false-negative.
3. **GitOps failure.** `git_ops_duration_seconds{status!="Success"}` rising means deployments cannot commit. Causes: expired git credentials, a protected branch, a full disk on the clone directory, or an unreachable git host.
4. **Downstream cluster unreachable.** A single bad cluster makes proxy paths hang and can exhaust handler capacity. `orchestrator_http_requests_current` climbs while CPU stays flat.
5. **OOM / restart.** `kube_pod_container_status_restarts_total{container="orchestrator"}` is the SRE team's own panel; a restarting orchestrator loses in-flight workflow state.

## Alerts you will actually see
`NatsOrchestratorConsumerConnectionBreaks`, `Container restarted{namespace="devtroncd"}`, `PodOOMKilled`, `CPUThrottlingHigh`.

## Remediation
- **5xx on one path:** get the path from `orchestrator_http_requests_total`, then read orchestrator logs filtered to it. Do not restart first — the path tells you the subsystem.
- **Everything slow, CPU flat:** look at Postgres (connections, locks, slow queries) and at `orchestrator_http_requests_current`. Restarting clears the symptom and loses the cause.
- **UI status stale but deploys work:** NATS consumer, not the orchestrator. Check `nats_consuming_count` by topic and the JetStream consumer's pending count.
- **GitOps stuck:** verify the git credential and that the target branch accepts commits; `git_ops_duration_seconds{operationName="CommitAndPushAllChanges"}` isolates it.
- Restart is a last resort and costs in-flight workflows. Say so when recommending it.
