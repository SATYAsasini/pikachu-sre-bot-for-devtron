---
name: ci-workflow
description: The per-build pods Devtron schedules in the devtron-ci namespace, driven
  by ci-runner inside an Argo Workflow.
---

# Devtron CI build workflows

## What it does
Each build runs as a pod in the **`devtron-ci`** namespace with the main container named `main`, orchestrated as an Argo Workflow and driven by ci-runner. Stages are pre-CI, build, post-CI, with cache download/upload around them.

## How it fails
1. **Pods Pending — no capacity.** `kube_pod_status_phase{namespace="devtron-ci",phase="Pending"}` above zero for minutes means the cluster cannot schedule builds: no Ready CI nodes, insufficient CPU/memory, or a node selector/taint mismatch. The SRE team tracks Ready CI nodes explicitly for this reason.
2. **CPU throttling.** `CPUThrottlingHigh` on build pods makes builds slow rather than failed. Users report "CI got slower" with no errors anywhere.
3. **OOMKilled build.** Large builds exceed the configured limit; the workflow fails with exit 137 and a generic message.
4. **Cache store unreachable.** `cache_download_duration_seconds` spikes while `build_duration_seconds` is normal — blob storage or its credentials, not the build.
5. **Image push failure.** Registry credentials or quota; surfaces at the end of a build that otherwise succeeded.

## Alerts you will actually see
`Ci-PodNotReady-override`, `CPUThrottlingHigh`, `PodOOMKilled`.

## Remediation
- **Pending builds:** check Ready node count and the CI node selector/taints before anything else. Scaling the CI node group is the fix; retrying the build is not.
- **Exit 137:** raise the build pod's memory limit for that pipeline. Say which pipeline.
- **Slow but succeeding:** compare `cache_download_duration_seconds` against `build_duration_seconds` to decide between blob store and build.
- Build pods are ephemeral: get evidence from the workflow pod's events and previous-container logs before it is garbage collected.
