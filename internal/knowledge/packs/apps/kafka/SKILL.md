---
name: kafka
description: Distributed log. Consumer lag and under-replicated partitions are the
  two signals that matter.
---

# Apache Kafka

## Top failure modes
1. **Consumer lag** — one group falls behind. Cause is the consumer (slow processing, crash loop, rebalance storm), not Kafka.
2. **Under-replicated partitions** — a broker is down, slow, or out of disk.
3. **Offline partitions** — genuine unavailability; usually multiple broker failures or a controller problem.
4. **Disk full on a broker** — retention misconfigured for the volume size.
5. **Rebalance storms** — consumers repeatedly joining/leaving; lag oscillates and never drains.

## Remediation
- Lag: fix or scale the consumer. Increasing partitions helps only if the consumer group can actually add members.
- Under-replicated: find the lagging broker; do not restart brokers in sequence without checking ISR recovery between each — that can cause offline partitions.
- Disk: adjust retention (`retention.ms` / `retention.bytes`) rather than deleting log segments by hand.
