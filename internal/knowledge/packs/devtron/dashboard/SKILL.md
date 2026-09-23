---
name: dashboard
description: The Devtron web UI. A static React app behind nginx — when "Devtron is
  down" it is usually not this.
---

# Devtron Dashboard

Container `dashboard`, namespace `devtroncd`.

## How it fails
Mostly it does not. When users say "Devtron is down", the dashboard pod is usually Ready and the real fault is the orchestrator behind it, the ingress, or an expired TLS certificate.

- **Blank page / assets 404** — a bad rollout or a CDN/ingress path issue.
- **UI loads, data missing** — orchestrator or its Postgres, not the dashboard.
- **502/504 at the ingress** — orchestrator unavailable or slow.

## Remediation
Before reporting a dashboard problem, confirm whether the orchestrator is answering. A correct RCA here usually reassigns the fault away from the dashboard, and that reassignment is the valuable part.
