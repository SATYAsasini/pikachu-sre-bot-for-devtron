---
name: mongodb
description: Document database. Connections, replica-set health and working-set memory
  dominate its failures.
---

# MongoDB

## Top failure modes
1. **No primary** — replica set lost its majority. Writes fail, reads may still work from secondaries. This is the one to check first on a multi-member deployment.
2. **Connection exhaustion** — clients without pooling.
3. **Working set exceeds WiredTiger cache** — latency rises steadily, no errors. Often misdiagnosed as a network problem.
4. **Disk full** — Mongo refuses writes.
5. **Missing index** — one slow query saturates CPU.

## Remediation
- Check replica-set member health before anything else on a multi-member set.
- A pod restart can trigger an election; in a two-member set it can lose the majority entirely. Flag that.
