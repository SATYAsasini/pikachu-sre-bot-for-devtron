---
name: kubewatch
description: Watches managed clusters with informers and publishes workload and workflow
  events onto NATS for the orchestrator to consume.
---

# Devtron Kubewatch

## What it does
Runs informers against every managed cluster and publishes what it sees to NATS. It is the reason the Devtron UI knows a pod restarted or a workflow finished. Container `kubewatch`, namespace `devtroncd`.

## How it fails — and why it is the most under-diagnosed component
Kubewatch failures are **silent**. The pod stays Ready, no HTTP endpoint 5xxes, and nothing in the UI says "events stopped". The only symptoms are absences: app status stuck on an old value, workflows that never move past Running, deployments that completed in the cluster but not in Devtron.

1. **Unreachable cluster** → `Kubewatch_unreachable_client_count` rises for one `clusterName`. Cause is usually a rotated or expired cluster credential, or the cluster API becoming unreachable from `devtroncd`.
2. **Informer unregistered** → `Kubewatch_unregistered_informer_count`. Often follows an API-server restart or a CRD change; the informer does not always recover on its own.
3. **NATS connection break** → alert `NatsKubewatchConsumerConnectionBreaks`. Events are produced but never delivered.
4. **Event flood** → `Kubewatch_non_administrative_events_count` spiking from a noisy namespace can starve the useful events and push NATS pending counts up.

## Alerts you will actually see
`NatsKubewatchConsumerConnectionBreaks`, `Container restarted{namespace="devtroncd"}`.

## Remediation
- **"Devtron UI is stale" with healthy workloads** is a kubewatch investigation, not an application one. Check the three counters above before anything else.
- Unreachable client on one cluster → re-check that cluster's credential in Devtron and network reachability from `devtroncd`.
- Unregistered informer → restarting kubewatch re-registers informers and is, unusually, the correct first action here.
- Always name which `clusterName` is affected; kubewatch degrades per cluster, not globally.
