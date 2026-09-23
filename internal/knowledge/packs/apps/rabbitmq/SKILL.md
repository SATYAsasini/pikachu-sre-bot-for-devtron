---
name: rabbitmq
description: Message broker. Queue depth, unacked messages and memory alarms cover
  most incidents.
---

# RabbitMQ

## The one that looks like an application hang
`rabbitmq_alarms_memory_used_watermark == 1` or the disk alarm. RabbitMQ **blocks publishing connections** rather than returning errors. Producers appear to hang, with no exception and no broker error log the application team will find. Check the alarms before investigating the producer.

## Top failure modes
1. Memory or disk alarm blocking publishers.
2. Zero consumers on a growing queue.
3. Unacked pile-up — consumer crashes before acking, message redelivers forever.
4. Network partition in a cluster — split brain, partial availability.

## Remediation
- Alarms: free memory/disk or raise the watermark; publishing resumes automatically once cleared.
- Unacked: fix consumer ack handling; consider a dead-letter queue for poison messages.
- Purging a queue destroys messages. Always flag it.
