# Claude Code Context: integration-nfeio

This is the **NFe.io adapter** — a standalone Yggdrasil integration worker
(`integration_type: nfeio`, namespace `global`) that wraps the NFe.io v2 REST
API to manage the lifecycle of Brazilian fiscal documents (NFSe service
invoices), and runs an HMAC-verified webhook listener for inbound NFe.io status
callbacks.

Repo: `github.com/dakasa-yggdrasil/integration-nfeio`.

> **Trust `Describe()` in `providers/nfeio/adapter/spec.go` over this file.**
> The adapter's live contract — capabilities, resource types, credential and
> instance schemas, transport, version — is whatever `Describe()` returns at
> runtime. This document is a map, not the source of truth. If anything below
> disagrees with `spec.go`, `spec.go` wins; fix this file. CI cross-checks
> `Describe()` against `SupportedExecuteOperations` via
> `cmd/lint-action-catalog` (`pkg/contractcheck`), so the spec stays honest
> about what `Execute()` actually handles.

## What this adapter does

Fiscal-document lifecycle on top of NFe.io (`domain: payments`):

- **Service invoices (NFSe):** issue, observe (read one or list), cancel,
  retrieve signed PDF/XML download URLs, and bulk-issue up to 50 at a time.
- **Companies:** ensure a company is registered at NFe.io, observe companies.
- **Webhook subscriptions:** ensure / observe / destroy NFe.io webhook
  subscriptions (the provider-side delivery config).
- **Municipalities + templates:** list NFe.io-supported municipalities
  (cached), and read/validate the in-memory município templates this repo
  ships under `manifest/templates/` (per-município ISS rules used to
  `calculate_iss`).
- **Webhook reactor:** a dedicated HTTP listener receives NFe.io callbacks,
  HMAC-verifies them, dedupes (LRU), normalizes the status, and republishes to
  the `enterprise-payments.*` queues via the `publish_message` capability on the
  rabbitmq-topology instance. This is **not** an `execute` op — it is fired by
  the webhook server, not the RPC bus.

## Transport & version

- **`AdapterVersion = "3.0.0"`** (in `spec.go`; also the default for the
  link-time-overridable `main.Version`).
- **Default transport is `http_json`** — RPC served on **port 8081**
  (`RPC_PORT`), routes `/rpc/describe` + `/rpc/execute`.
- **AMQP is opt-in**: set `YGGDRASIL_TRANSPORT=amqp` (or `rabbitmq`) and provide
  `BROKER_URL`; the SDK then serves describe/execute on the
  `yggdrasil.adapter.nfeio.{describe,execute}` queues. `Describe()` reports
  `rabbitmq` + queue names in that mode, `http_json` + endpoints otherwise.
- Three listeners total (see `cmd/adapter/main.go`):
  1. **RPC** (HTTP `:8081` or AMQP) — describe + execute, via `yggdrasil-sdk-go`.
  2. **Health** (`:8080`, `HEALTHCHECK_PORT`) — `/healthz`, `/readyz`,
     `/metrics`. Both probes return 200; reconnect is handled inside the
     transport, so readiness is effectively "templates loaded, loop running".
  3. **Webhook** (`:8082`, `WEBHOOK_PORT`) — inbound NFe.io callbacks at
     `/webhook/nfeio`.

## Capabilities (canonical names — see `spec.go`)

14 callable `execute` ops + 1 reactor. The canonical names follow the v2.0.0
capability convention (`ensure_/observe_/destroy_/discover_` prefixes for
resource lifecycles, with documented helper/action exceptions).

| Canonical capability | Resource | Notes |
|---|---|---|
| `ensure_service_invoice` | service_invoice | issue NFSe; 409 duplicate → idempotent success |
| `observe_service_invoices` | service_invoice | filter by `{id}` (one) or paginate (list) |
| `destroy_service_invoice` | service_invoice | cancel NFSe; 404 → already-absent success |
| `retrieve_pdf` | service_invoice | allowlisted helper — signed PDF URL |
| `retrieve_xml` | service_invoice | allowlisted helper — signed XML URL |
| `ensure_company` | company | register at NFe.io; 409 → idempotent success |
| `observe_companies` | company | filter by `{federal_tax_number}` or list |
| `observe_municipalities` | municipality | cached 1h, stale-while-error |
| `manage_template` | municipality_template | control-plane: get/list/validate templates |
| `bulk_issue` | service_invoice | bulk action — up to 50, semaphore 5, partial-failure |
| `calculate_iss` | municipality_template | pure-function helper — ISS from template |
| `ensure_webhook_subscription` | webhook_subscription | exact-ID GET/PUT; only `insecureSsl=false` |
| `observe_webhook_subscriptions` | webhook_subscription | exact `{id}` only; no list/discovery |
| `destroy_webhook_subscription` | webhook_subscription | exact ID + matching `confirm_id`; not a default action |
| `nfse_webhook_received` *(reactor)* | service_invoice | NOT an execute op — webhook-server-triggered |

The four non-prefix names (`retrieve_pdf`, `retrieve_xml`, `manage_template`,
`bulk_issue`, `calculate_iss`) are **kept by allowlist** — they are helpers,
control-plane actions, or bulk/pure-function operations, not CRUD on an external
resource. The rationale is documented inline in `actionCatalog()` in `spec.go`.

### Legacy aliases

The one-minor compatibility aliases from v2 were removed at the v3.0.0 major
boundary. Only canonical capability names are accepted.

## Repo layout

```
cmd/adapter/                  # main binary: 3 listeners (RPC/health/webhook); embeds templates/*.yaml
cmd/adapter/templates/        # build-time copy of manifest/templates (Go embed can't escape '..')
cmd/validate-templates/       # CI gate: fails on any município-template schema error
cmd/lint-action-catalog/      # CI gate: Describe() vs SupportedExecuteOperations drift (contractcheck)
providers/nfeio/adapter/      # the adapter — see "Where things live" below
providers/nfeio/config/       # env loading (config.go): API key, webhook secret, ports, base URL
family/contract/              # local contract types (AdapterDescribeResponse, schema specs, etc.)
pkg/contractcheck/            # public lint pkg used by cmd/lint-action-catalog
manifest/                     # integration_type + per-capability + instance + reactor YAML; templates/
manifest/templates/           # per-município ISS template YAML (3106200, 3304557, 3550308, 4106902, 4205407)
deploy/                       # deployment artifacts
docs/                         # CAPABILITIES / CONFIGURATION / DEVELOPMENT / OPERATIONS / USAGE
integration_tests/            # integration test harness
yggdrasil-quickstart.yaml     # one-shot installer bundle (yggdrasil install …)
Dockerfile                    # golang:1.25 → distroless; EXPOSE 8080 8081 8082
```

Key files in `providers/nfeio/adapter/`:

- `spec.go` — `Describe()` contract, capability constants, `SupportedExecuteOperations`, action catalog, Prometheus metrics. **Source of truth.**
- `adapter.go` — `DescribeHandler`, `ExecuteHandler`, `executeRoute` switch for canonical ops and allowlisted actions.
- `reconcilers.go` — `WireReconcilersWithInstance`: installs the SDK reconcile dispatch (ensure/observe/destroy triples + §6.5 mutation-event emission) ahead of the action switch.
- `client.go` / `bearer.go` — NFe.io v2 HTTP client + auth.
- `webhook_server.go` — inbound webhook listener: HMAC verify → LRU dedup → normalize → publish.
- `publish_dispatch.go` — republishes normalized webhook events to `enterprise-payments.*`.
- `issue_nfse.go` / `cancel_nfse.go` / `get_nfse_status.go` / `register_company.go` / `bulk_issue.go` / `retrieve_pdf.go` / `retrieve_xml.go` / `list_municipalities.go` / `manage_template.go` / `tax_calc.go` / `template_loader.go` / `webhook_subscription.go` — per-operation logic (filenames still use legacy NFSe verbs).

## Credentials & instance config

From `Describe()` / `config.go`:

- **Credentials (required):** `NFEIO_API_KEY` (REST auth), `NFEIO_WEBHOOK_SECRET`
  (HMAC verify of inbound webhooks). Both are secret/sensitive. `config.Load()`
  fails fast (process exits) if either is empty.
- **Instance schema:** `environment` enum `production` | `sandbox` (default
  `production`).
- **Other env knobs:** `NFEIO_COMPANY_ID` (optional default company; per-call
  override allowed), `NFEIO_BASE_URL` (default `https://api.nfe.io`),
  `RPC_PORT` (8081), `WEBHOOK_PORT` (8082), `HEALTHCHECK_PORT` (8080),
  `TEMPLATES_DIR` (default `manifest/templates`; falls back to the binary's
  embedded templates if absent), `YGGDRASIL_TRANSPORT`, `BROKER_URL`,
  `YGGDRASIL_CORE_URL`, `RABBITMQ_TOPOLOGY_INSTANCE`, `YGGDRASIL_RUN_TOKEN`.

## Webhook security

The webhook server (`webhook_server.go`) reads the raw body *before* JSON
parsing so the HMAC covers the exact bytes NFe.io signed. It verifies
`X-Hub-Signature-256` (falling back to `X-Hub-Signature`) against
`NFEIO_WEBHOOK_SECRET` using `sdkwebhook.VerifyHMACSHA256Header`; a bad
signature is `401`. After verify, it dedupes by event id (or a SHA-256 of the
body when no id is present) through an LRU cache, then normalizes and publishes.

## Mandatory adapter rules

- **Keep `Describe()` aligned with `Execute()`.** `cmd/lint-action-catalog`
  (`pkg/contractcheck`) is a CI gate — don't silence it. Every catalog entry has
  a matching `manifest/capability.*.yaml`.
- **Keep the worker standalone.** Don't import runtime/domain code from
  yggdrasil-core or the monorepo. Use `yggdrasil-sdk-go` + the local
  `family/contract` types only.
- **Rename/add a capability → update `spec.go`, the matching
  `manifest/capability.*.yaml`, tests, `docs/`, and the README in the same
  change.** Adding a *new* capability with a `create_/list_/delete_/update_`
  prefix is the wrong move — use the `ensure_/observe_/destroy_/discover_`
  convention.
- **Fail fast over silent degradation.** No swallowing errors; 404s map to
  already-absent success deliberately, terminal failures (e.g. 422
  `cancellation_window_closed`) stay failures.

## Manifest ↔ `spec.go`

`manifest/integration_type.nfeio.yaml` is a static snapshot of `Describe()` and
is currently **in sync** with `spec.go` (version `3.0.0`, no `register_company`
default action, `thirdparty.nfeio.municipality_template` prefix,
`.{external_id}`/`.{federal_tax_number}`/`.{code}` identity templates, upper-case
credential keys, `environment` enum). **`Describe()` is authoritative; do not
"fix" the code to match the manifest** — re-derive the manifest from `Describe()`
instead. No describe-dump tool exists here, so when the contract changes,
hand-sync the snapshot in the same change and run the lint/spec gates.

## Validation

```bash
go test ./...
go run ./cmd/validate-templates manifest/templates   # template-schema gate
go run ./cmd/lint-action-catalog                      # describe ↔ execute gate
docker build --build-arg VERSION=$(git rev-parse --short HEAD) .
```

CI (`.github/workflows/`): `ci.yml` (test + lint gates), `release.yml`
(publishes the worker image to `ghcr.io/dakasa-yggdrasil/integration-nfeio`),
`emit-deploy-event.yml` (POSTs the deploy event into yggdrasil-core).
