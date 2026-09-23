<p align="center"><img src="web/public/mark.png" width="96" alt=""></p>

# Pikachu — SRE bot for Devtron

A second-layer SRE for Kubernetes alerts.

Devtron already has a first-pass debugger (`/proxy/athena/intelligence`). It gathers context and
proposes a cause, but it doesn't check its own answer and it doesn't write remediation an on-call
engineer can act on. Pikachu does both: it **verifies** Devtron's answer against facts read from the
cluster, then **adds** ranked, reversible remediation.

It is read-only, needs no kubeconfig, and reaches every cluster through Devtron.

---

## How it works

You pick a cluster and an alert (or describe a problem) in the dashboard. That creates a **run**.

```
  alert / question
        │
        ▼
 ┌──────────────┐   cluster unreachable? → stop early, say why
 │ 0. preflight │
 └──────┬───────┘
        ▼
 ┌──────────────┐   Prometheus or VictoriaMetrics? Alertmanager or vmalert?
 │ 1. discover  │   found per cluster, never configured
 └──────┬───────┘
        ▼
 ┌──────────────┐   target object, events, Devtron app state, related alerts,
 │ 2. facts     │   component identified from chart / image / labels
 └──────┬───────┘   — deterministic, no model involved
        ▼
 ┌──────────────┐   Devtron's own analysis, streamed live into the run
 │ 3. first pass│   (if it fails, the run continues and says so)
 └──────┬───────┘
        ▼
 ┌──────────────┐   grades Devtron's answer claim by claim against the facts
 │ 4. judge     │   → verdict: supported / partly / unsupported / insufficient
 └──────┬───────┘   + gaps. Fast model, no tools, one call
        │
        │  settled and nothing to check?  → skip the deep dive
        ▼
 ┌──────────────┐   closes the gaps with read-only tools, then writes
 │ 5. sre       │   ranked remediation: risk, how to verify, how to roll back
 └──────┬───────┘   strong model, tool + token budget enforced
        ▼
   report + evidence ledger, streamed to the UI over SSE
```

**Run depth** decides how far it goes:

| Depth | Behaviour |
|---|---|
| `auto` | the judge decides whether the deep dive is needed |
| `quick` | stop after the verdict |
| `deep` | always run the deep dive |

There is also a **chat** panel: a quick question sent straight to Devtron's first pass, streamed
back. The browser keeps the chat; the server stores nothing.

### What makes the answers trustworthy

- **Missing data is never reported as healthy.** If metrics were unreachable, the report says
  coverage is unknown. It never says "memory looks fine".
- **Every claim cites evidence.** Findings reference ledger entries as `[ev:N]`. Anything that
  can't be cited is listed as unknown.
- **Components are identified, not guessed.** A workload is matched by Helm chart, then image,
  then labels. A pod name containing "redis" isn't evidence.
- **Built-in knowledge, read from source.** 20 embedded packs: 12 Devtron services (with the
  Prometheus metrics each one exposes) and 8 common products (Postgres, Redis, Kafka, MongoDB,
  MySQL, RabbitMQ, Elasticsearch, ingress-nginx).
- **Unknown components are still measured.** For anything outside the packs, it discovers what
  the workload actually exports in *this* cluster and reads the scrape config, so it can tell
  "nothing is wrong" from "nobody is watching".

---

## Architecture

One binary, one process, one Postgres.

```
            browser
               │  same origin
               ▼
 ┌─────────────────────────────────────────────┐
 │  sre-agent  (:8090)                         │
 │                                             │
 │   /            dashboard (embedded)         │
 │   /v1/*        REST + SSE                   │
 │                                             │
 │   worker pool ── judge ─► sre   (Google ADK)│
 │        │                                    │
 │   read-only tools · knowledge packs ·       │
 │   redaction · budget guard                  │
 └──────┬──────────────────┬──────────────┬────┘
        │                  │              │
        ▼                  ▼              ▼
   Postgres          Devtron API      model provider
   runs, ledger,     every cluster    Anthropic or Gemini
   settings,         read goes here
   agent sessions        │
                         ▼
                 clusters · Prometheus / VictoriaMetrics
                 · Alertmanager / vmalert
```

- **Two agents only**, `judge` then `sre`, run as an ADK sequential agent. They share nothing
  except session state.
- **ADK runs as a library** inside the binary. The only outbound AI call is model inference.
  Session state lives in Postgres, so a run's reasoning survives a restart.
- **A guard** wraps every model and tool call: per-agent tool allowlist, tool and token budgets,
  and the audit ledger.

### Access model

There's no kubeconfig. Devtron's RBAC decides what the token can see. One view-only token is sent
three different ways; getting this wrong is the usual cause of a 401:

| Placement | Used for |
|---|---|
| `token: <t>` header | every `/orchestrator/*` call |
| `Authorization: Bearer <t>` | `/orchestrator/k8s/proxy/*` only |
| `Cookie: argocd.token=<t>` | `/proxy/athena/intelligence` only |

The Kubernetes proxy is GET-only, and every tool is read-only. There are no write tools.

### Code layout

```
cmd/sre-agent        the binary: API, SSE, worker pool
internal/api         /v1 contract
internal/webui       embedded dashboard
internal/worker      preflight → facts → first pass → agents
internal/agents      judge + sre pipeline, guard, model selection
internal/tools       read-only tools
internal/devtron     the only path to any cluster
internal/monitoring  alerts + metrics across Prometheus / VictoriaMetrics
internal/knowledge   component identification + embedded packs
internal/runs        run record, evidence ledger, migrations
internal/redact      secret redaction before anything reaches a model
prompts/             judge.md, sre.md (embedded)
web/                 React dashboard
```

---

## Deploy

One image, one container, port **8090**. The dashboard, prompts and knowledge packs are all inside
the binary.

```sh
docker build -t pikachu-sre-bot .
```

The only other thing it needs is **Postgres 16** (no extensions). Migrations run on start.

| Variable | Required | Notes |
|---|---|---|
| `SRE_DATABASE_URL` | yes | `postgres://user:pass@host:5432/db?sslmode=disable` |
| `ANTHROPIC_API_KEY` or `SRE_GEMINI_API_KEY` | yes | provider is picked from whichever is set |
| `SRE_DEVTRON_URL` | no | can be saved from the Settings screen instead |
| `SRE_DEVTRON_TOKEN` | no | view-only token; can be saved from Settings |
| `SRE_MODELS_FAST` / `SRE_MODELS_STRONG` | no | model ids for judge / sre |
| `SRE_RUN_CONCURRENCY` | no | parallel runs, default 4 |

Health check: `GET /v1/healthz`. Every key in `config.example.yaml` maps to an `SRE_*` variable.

Things to get right:

- **Run exactly one replica, with the `Recreate` strategy.** On start, the app marks any
  unfinished runs as failed, and the live stream is held in memory. A second pod kills
  in-flight investigations.
- **Turn proxy buffering off** on the ingress and set a long read timeout, or the live run page
  arrives all at once.
- **There's no built-in login.** Keep it internal, or put an authenticating proxy in front.
  `X-Forwarded-User` is recorded on settings changes.

---

## Development

Requires Go 1.26, Node 22, Postgres, and a model API key.

```sh
cp .env.example .env        # SRE_DATABASE_URL, ANTHROPIC_API_KEY, optional Devtron URL/token
make run                    # API on :8090, migrates on start (.env is read automatically)
```

Dashboard, two options:

```sh
cd web && npm install && npm run dev     # hot reload on :5173, proxies /v1 to :8090
make web && make run                     # build the UI into the binary, serve on :8090
```

Checks:

```sh
go build ./... && go vet ./... && go test ./...
cd web && npm run lint && npm run build
```

Tests never touch a live Devtron, Prometheus or Alertmanager: external HTTP is faked with
`httptest`, and reusable fixtures live in each package's `fixtures_test.go`.
