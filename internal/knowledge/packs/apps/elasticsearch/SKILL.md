---
name: elasticsearch
description: Search and log store. Cluster status colour and disk watermarks explain
  almost every incident.
---

# Elasticsearch / OpenSearch

## Top failure modes
1. **Red cluster** — primary shards unassigned. Data is genuinely unavailable. Usually a node loss or a failed allocation.
2. **Disk watermark** — at 85% shards stop allocating; at 95% the **flood stage sets indices read-only**. Writes fail with an obscure error and the cluster still reports yellow/green. This is the most commonly misdiagnosed Elasticsearch incident.
3. **JVM heap pressure** — long GC pauses, then node drop-outs.
4. **Thread pool rejections** — overload.

## Remediation
- Red → find the unassigned shard's reason via allocation explain before acting.
- Flood-stage read-only → free disk, then explicitly clear the read-only block; freeing disk alone does not lift it. Say both steps.
- Heap → JVM heap should be ≤50% of the container limit and under ~31GB.
