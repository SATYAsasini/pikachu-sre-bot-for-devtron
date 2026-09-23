---
name: nats
description: The message bus every Devtron microservice uses. Most "Devtron is stuck"
  incidents are NATS incidents.
---

# NATS / JetStream in Devtron

## Why this matters more than it looks
Devtron is event-driven. Orchestrator, kubewatch, git-sensor, ci-runner and the CD workflow all coordinate over NATS topics. When NATS degrades, **every service stays green** and the product stops working. A pod-level investigation of any single Devtron component will find nothing.

Topics seen in practice include `CI-COMPLETE`, `CD-STAGE-COMPLETE`, workflow status topics and `PANIC-ON-PROCESSING-TOPIC`.

## The diagnostic that settles it
Per topic, compare three counters:

- `nats_publish_count` — produced
- `nats_consuming_count` — consumption started
- `nats_consumption_count` — consumption finished

| Pattern | Meaning |
|---|---|
| publish flat, was non-zero | The producer stopped. Look at kubewatch or the orchestrator, not NATS. |
| publish rising, consuming flat | No consumer is attached. A consumer connection broke. |
| consuming rising, consumption flat | Consumers start and never finish — they are erroring, timing out or panicking. |
| all three matched, `nats_event_delivery_count` high | Redelivery loop; a consumer is failing after partial work. |
| `nats_publish_error_count` non-zero | Events lost at source; downstream will never converge on its own. |

## Alerts you will actually see
`NatsPendingMessages`, `NatsMessagesAckPending`, `NatsKubewatchConsumerConnectionBreaks`, `NatsOrchestratorConsumerConnectionBreaks`.

## Remediation
- **Pending / ack-pending rising:** identify the slow consumer with `nats_event_consumption_time` by topic, then fix or scale that consumer. Purging the stream loses events and should be stated as destructive.
- **Consumer connection breaks:** restart the affected consumer (kubewatch or orchestrator); confirm recovery by watching `nats_consumption_count` for that topic resume.
- **Panic topic active:** `nats_publish_count{topic="PANIC-ON-PROCESSING-TOPIC"}` increasing means a service is panicking during event processing. Get the panic from that service's logs; this is a code-level bug, not a capacity problem.
- Never recommend "restart NATS" as a first action: JetStream restarts can lose un-acked messages and will not fix a broken consumer.
