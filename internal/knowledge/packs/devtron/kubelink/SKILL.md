---
name: kubelink
description: gRPC service that performs all Helm operations and watches release state
  in managed clusters on the orchestrator's behalf.
---

# Devtron Kubelink

## What it does
Every Helm install, upgrade, rollback and release-status read goes through kubelink over gRPC. It also runs informers per cluster to keep release state current. Container `kubelink`, namespace `devtroncd`.

## How it fails
1. **Target cluster unreachable or throttled.** `grpc_server_handling_seconds` p99 rises and `grpc_server_handled_total{grpc_code!="OK"}` grows, usually with `grpc_code="DeadlineExceeded"` or `Unavailable`. The fault is the managed cluster, not kubelink.
2. **Informer churn.** `kubelink_informer_data_transform_duration_seconds` climbing on one `clusterName` means that cluster has a large or rapidly changing release set; memory follows and OOM comes next.
3. **OOMKill under many releases.** Informers hold release state in memory; a cluster with thousands of Helm releases is the usual trigger.
4. **Helm operation failures surfacing as deploy failures.** The UI shows a failed deployment; the actual error text is in kubelink's logs, not the orchestrator's.

## Remediation
- Read `grpc_code` first. `Unavailable`/`DeadlineExceeded` → investigate the target cluster's API server, not Devtron. `Internal` → kubelink logs carry the Helm error.
- Memory growth tracking release count → raise the kubelink memory limit; restarting only defers it.
- Never conclude "kubelink is broken" from HTTP metrics alone; its work is gRPC and the HTTP surface is mostly health checks.
