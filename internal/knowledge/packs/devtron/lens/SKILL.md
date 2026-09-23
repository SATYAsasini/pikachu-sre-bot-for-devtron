---
name: lens
description: ' Deployment metrics and DORA-style analytics service. Non-critical:
  when it fails, only charts break.'
---

# Devtron Lens

Container `lens`, namespace `devtroncd`. Computes deployment frequency and lead-time analytics from CD events.

## How it fails
Restarts and NATS consumption gaps. Lens consumes deployment events; a NATS break leaves analytics permanently missing a window even after recovery.

## Remediation
Worth stating explicitly in any RCA: **lens being down does not affect deployments.** If lens is the only unhealthy component, the platform is fine and this is a low-severity issue. Do not let a lens alert drive an urgent response.
