<div align="center">

# integration-nfeio

**Yggdrasil integration adapter for [NFe.io](https://nfe.io) — Brazilian municipal service invoices (NFSe).**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](./LICENSE)
[![Go](https://img.shields.io/badge/go-1.25-00ADD8.svg)](./go.mod)
[![Image](https://img.shields.io/badge/ghcr.io-integration--nfeio-blue.svg)](https://github.com/dakasa-yggdrasil/integration-nfeio/pkgs/container/integration-nfeio)

Issue, observe, cancel and reconcile NFSe service invoices through a declarative Yggdrasil integration · [Usage](./docs/USAGE.md) · [Configuration](./docs/CONFIGURATION.md) · [Capabilities](./docs/CAPABILITIES.md) · [Operations](./docs/OPERATIONS.md) · [Development](./docs/DEVELOPMENT.md)

</div>

---

## What it is

`integration-nfeio` is a self-contained Yggdrasil integration worker that wraps the
[NFe.io](https://nfe.io) REST API (`/v2/...`) so you can manage Brazilian municipal
service invoices (NFSe) declaratively. It speaks the standard adapter `describe` /
`execute` contract over HTTP-JSON, exposes 14 callable capabilities across five
resource types (service invoices, companies, municipalities, município templates,
webhook subscriptions), and runs a dedicated webhook listener that turns inbound
NFe.io status callbacks into `enterprise-payments.nfe.*` queue messages via the
`nfse_webhook_received` reactor.

> **Where this fits in Yggdrasil** — Yggdrasil is a self-hosted control plane for
> declarative workflows + integrations over your whole stack (think *Backstage, but
> more complete and scalable*). You write YAML; Yggdrasil persists, runs, and audits
> it. This repo is one **leaf integration adapter**: `yggdrasil-core` owns the catalog,
> RBAC and workflow engine, and calls this adapter's `describe`/`execute` endpoints to
> drive NFe.io. See [`yggdrasil-core`](https://github.com/dakasa-yggdrasil/yggdrasil-core).

## Where it fits

```mermaid
flowchart LR
  core["yggdrasil-core<br/>(catalog · RBAC · workflows)"]
  adapter["integration-nfeio<br/>(this adapter)"]
  nfeio["NFe.io REST API<br/>api.nfe.io /v2"]

  core -- "HTTP-JSON<br/>/rpc/describe · /rpc/execute" --> adapter
  adapter -- "REST + API key" --> nfeio
  nfeio -. "HMAC webhook<br/>/webhook/nfeio" .-> adapter
  adapter -- "publish_message → rabbitmq-topology" --> core
```

### Integration model

```mermaid
flowchart TD
  family["family: nfeio"]
  itype["integration_type: nfeio<br/>provider=nfeio · domain=payments · transport=http_json"]
  instance["instance: nfeio-dakasa<br/>(namespace dakasa)"]
  provider["provider impl: providers/nfeio/adapter"]

  family --> itype --> instance --> provider

  provider --> r1["service_invoice"]
  provider --> r2["company"]
  provider --> r3["municipality"]
  provider --> r4["municipality_template"]
  provider --> r5["webhook_subscription"]
```

## Capabilities

14 callable capabilities (via `execute`) plus 1 reactor (triggered by the inbound
webhook server, **not** callable via `execute`). All are idempotent. Full I/O
schemas in [docs/CAPABILITIES.md](./docs/CAPABILITIES.md).

| Capability | Resource type | What it does |
|---|---|---|
| `ensure_service_invoice` | `service_invoice` | Issue an NFSe; 409 duplicate → idempotent success |
| `observe_service_invoices` | `service_invoice` | Read one (`{id}`/`{invoice_id}`) or paginate |
| `destroy_service_invoice` | `service_invoice` | Cancel an emitted NFSe; 404 → already-absent |
| `retrieve_pdf` | `service_invoice` | Signed PDF download URL (allowlisted helper) |
| `retrieve_xml` | `service_invoice` | Signed XML download URL (allowlisted helper) |
| `bulk_issue` | `service_invoice` | Bulk-issue **up to 50** NFSe, semaphore 5, partial-failure |
| `ensure_company` | `company` | Register a company; 409 already-registered → success |
| `observe_companies` | `company` | Read one (`{id}`) or paginate / filter by tax number |
| `observe_municipalities` | `municipality` | List NFe.io municipalities (cached 1h) |
| `manage_template` | `municipality_template` | Read-only `get` / `list` / `validate` on bundled templates |
| `calculate_iss` | `municipality_template` | Pure-function ISS tax computation (no network) |
| `ensure_webhook_subscription` | `webhook_subscription` | Exact-ID GET/PUT; may only set `insecureSsl=false` |
| `observe_webhook_subscriptions` | `webhook_subscription` | Read one exact `{id}`; no enumeration |
| `destroy_webhook_subscription` | `webhook_subscription` | Exact-ID delete with matching `confirm_id`; 404 is success |
| `nfse_webhook_received` *(reactor)* | `service_invoice` | Inbound NFe.io webhook → normalize → publish to `enterprise-payments.nfe.*` |

> Capability names follow the Yggdrasil `ensure_/observe_/destroy_` convention.
> The pre-v2.0.0 compatibility aliases were removed at the v3.0.0 major boundary.
> See [CHANGELOG.md](./CHANGELOG.md).

## Quick start

This adapter ships as a container image; there is no bundled `yggdrasil-quickstart.yaml`
or `docker-compose.yml` in the repo. Run the worker directly:

```bash
# 1. Build (or pull ghcr.io/dakasa-yggdrasil/integration-nfeio)
docker build -t integration-nfeio:dev .

# 2. Run — HTTP-JSON transport is the default
docker run --rm \
  -e NFEIO_API_KEY=sk_live_xxx \
  -e NFEIO_WEBHOOK_SECRET=whsec_xxx \
  -p 8080:8080 -p 8081:8081 -p 8082:8082 \
  integration-nfeio:dev

# 3. Verify
curl localhost:8080/healthz          # -> ok
curl localhost:8081/rpc/describe     # -> adapter contract JSON
```

Register it with `yggdrasil-core` by applying the manifests under `manifest/`
(integration type, instance, capabilities) — see [docs/USAGE.md](./docs/USAGE.md).

## Configuration

Two mandatory secrets; the rest have safe defaults. Full table in
[docs/CONFIGURATION.md](./docs/CONFIGURATION.md).

| Env var | Required | Secret | Default | Purpose |
|---|:---:|:---:|---|---|
| `NFEIO_API_KEY` | yes | yes | — | NFe.io REST API key |
| `NFEIO_WEBHOOK_SECRET` | yes | yes | — | HMAC-SHA256 secret for inbound webhooks |
| `NFEIO_BASE_URL` | no | no | `https://api.nfe.io` | NFe.io API base URL |
| `NFEIO_COMPANY_ID` | no | no | _(empty)_ | Default company; per-call override allowed |
| `YGGDRASIL_TRANSPORT` | no | no | `http_json` | `http_json` (default) or `amqp` |
| `RPC_PORT` | no | no | `8081` | HTTP RPC port (`/rpc/describe`, `/rpc/execute`) |
| `HEALTHCHECK_PORT` | no | no | `8080` | Health + `/metrics` port |
| `WEBHOOK_PORT` | no | no | `8082` | Inbound webhook port (`/webhook/nfeio`) |

## Usage

A workflow step issuing a São Paulo NFSe (real capability + município template code):

```yaml
- id: issue-invoice
  uses: integration.nfeio.ensure_service_invoice
  with:
    municipio_code: "3550308"            # São Paulo (bundled template)
    external_id: "order-9c2f"
    borrower_name: "Acme Servicos LTDA"
    borrower_federal_tax_number: 12345678000190
    borrower_address: { street: "Av Paulista", number: "1000", city: "São Paulo" }
    service_amount: 1500.00
    description: "Consultoria de software"
```

Full end-to-end journey (install → configure → first run → verify) in
[docs/USAGE.md](./docs/USAGE.md).

## Webhooks & reactors

NFe.io POSTs status callbacks to the adapter's webhook listener (port `8082`,
path `/webhook/nfeio`). The `nfse_webhook_received` reactor HMAC-verifies the body
(`X-Hub-Signature-256`), dedupes (LRU, 4096 entries), normalizes the event to
`{issued | cancelled | processing_failed}`, and publishes to the matching
`enterprise-payments.nfe.*` queue via the `publish_message` capability on the
`rabbitmq-topology` instance.

```mermaid
sequenceDiagram
  participant NFe as NFe.io
  participant WH as Webhook server :8082<br/>/webhook/nfeio
  participant RX as nfse_webhook_received<br/>(reactor)
  participant Core as yggdrasil-core<br/>/api/v1/capabilities/invoke
  participant Q as enterprise-payments.nfe.*.q

  NFe->>WH: POST /webhook/nfeio (X-Hub-Signature-256)
  WH->>RX: verify HMAC · LRU dedup · normalize event
  alt duplicate
    RX-->>NFe: 200 {"status":"duplicate"}
  else known event
    RX->>Core: publish_message (instance=rabbitmq-topology)
    Core->>Q: enqueue (emitted | rejected | canceled)
    RX-->>NFe: 202 Accepted
  else unknown event
    RX-->>NFe: 202 Accepted (logged, not enqueued)
  end
```

See [docs/CAPABILITIES.md](./docs/CAPABILITIES.md#reactor-nfse_webhook_received) and
[docs/OPERATIONS.md](./docs/OPERATIONS.md#webhook-runbook).

## Operations

- Liveness: `GET :8080/healthz` → always `200 ok`.
- Readiness: `GET :8080/readyz` → always `200 ready` (transport reconnect handled internally).
- Metrics: `GET :8080/metrics` → 7 Prometheus series (`nfeio_*`).

Runbook and failure modes in [docs/OPERATIONS.md](./docs/OPERATIONS.md).

## Development

```bash
go test ./...                              # unit tests
go run ./cmd/lint-action-catalog           # describe/execute/catalog drift gate
go run ./cmd/validate-templates ./manifest/templates/
go vet ./...
docker build -t integration-nfeio:ci .
```

Repo layout, the describe/execute contract, and `pkg/contractcheck` are covered in
[docs/DEVELOPMENT.md](./docs/DEVELOPMENT.md).

## Compatibility

- Go **1.25**.
- `yggdrasil-sdk-go` **v0.8.3** (`adapter`, `webhookhttp`, `sdk/reconcile`, `sdk/events`).
- Adapter version reported by `Describe()`: **3.0.0** (`AdapterVersion` in `providers/nfeio/adapter/spec.go`).
- Transport: HTTP-JSON (default) or AMQP, selected by `YGGDRASIL_TRANSPORT`.

## License

[Apache-2.0](./LICENSE).
</content>
</invoke>
