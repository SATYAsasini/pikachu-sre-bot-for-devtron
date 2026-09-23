---
name: nginx-ingress
description: Cluster edge. Its metrics tell you whether a problem is the edge or the
  backend behind it.
---

# NGINX Ingress Controller

## Reading the status code
| Code | Meaning | Where to look |
|---|---|---|
| 502 | Backend returned an invalid response or died mid-request | the backend pod |
| 503 | No endpoints available | Service selector, pod readiness |
| 504 | Backend too slow | backend latency, proxy timeout annotations |
| 413 | Body too large | `proxy-body-size` annotation |
| 499 | Client gave up first | usually downstream slowness |

## The one people miss
`nginx_ingress_controller_config_last_reload_successful == 0`. A malformed annotation on *any* Ingress breaks the reload for *all* of them. The controller keeps serving the last good config, so everything works — until a new Ingress or certificate silently fails to apply. Check this whenever a routing change "did not take effect".

## Remediation
- 503 with a healthy pod → Service selector or readiness probe, not the ingress.
- Certificate expiry → check cert-manager, not nginx.
- Failed reload → find the offending annotation in the controller logs; fixing one Ingress fixes all of them.
