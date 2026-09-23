---
name: image-scanner
description: Scans built images for vulnerabilities. Failures block CI completion
  even though the build itself succeeded.
---

# Devtron Image Scanner

Container `image-scanner-new`, namespace `devtroncd`.

## How it fails
- **Vulnerability DB download fails** (no egress, rate limit) — every scan fails identically and at once.
- **Registry credentials** — scans fail for one registry only.
- **Large images / timeouts** — `image_scanner_http_duration_seconds` tail grows; CI appears to hang at the scan stage.
- **OOM on big images.**

## Remediation
Distinguish "all scans failing" (DB or egress) from "one repo failing" (registry credential). If scanning is blocking releases and the cause is infrastructure rather than a real CVE, say so plainly — the safe short-term action is a scoped scan bypass, which is a policy decision for a human.
