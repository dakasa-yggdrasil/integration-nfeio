# Configuration — integration-nfeio

Every credential, instance field, and runtime env var the worker reads. Sourced from
`providers/nfeio/adapter/spec.go` (`Describe()`), `providers/nfeio/config/config.go`,
`cmd/adapter/main.go`, and `manifest/integration_type.nfeio.yaml`.
Back to the [README](../README.md).

---

## Credential schema

Advertised by `Describe()` with `mode: inline`. Both fields are **required** and
**secret**. `yggdrasil-core` resolves them per-instance (e.g. from a secret store)
and injects them into the worker's environment as the env vars below.

| Contract field | Runtime env var | Type | Required | Secret | Purpose |
|---|---|---|:---:|:---:|---|
| `nfeio_api_key` | `NFEIO_API_KEY` | string | yes | yes | NFe.io REST API key (NFe.io → Settings → API). Authenticates every `/v2` call. |
| `nfeio_webhook_secret` | `NFEIO_WEBHOOK_SECRET` | string | yes | yes | 32 to 64 character HMAC-SHA1 secret. It verifies inbound `/webhook/nfeio` callbacks and is the runtime-only source for an explicitly confirmed provider security migration. |

> The manifest (`manifest/integration_type.nfeio.yaml`), wire `Describe()`
> contract, and integration instance use canonical lower-case keys. Deployment
> wiring maps those keys to the worker's unchanged upper-case `NFEIO_*` environment variables.

## Instance schema

Advertised by `Describe().InstanceSchema` (`mode: inline`):

| Field | Type | Required | Default | Notes |
|---|---|:---:|---|---|
| `environment` | string (enum) | no | `production` | One of `production` / `sandbox`. UI metadata only; the worker selects the API host via `NFEIO_BASE_URL`, not this field. |

> The YAML manifest's `instance_schema.properties` is empty (`{}`); the `environment`
> property is declared in the live `Describe()` spec. This is a manifest-vs-code
> drift — the wire spec is authoritative for what surfaces render.

The shipped instance manifest (`manifest/instance.nfeio.yaml`) additionally carries a
`config` block consumed as env vars by the worker:

| Config key | Maps to env var | Default | Notes |
|---|---|---|---|
| `NFEIO_BASE_URL` | `NFEIO_BASE_URL` | `https://api.nfe.io` | NFe.io API base URL. |
| `NFEIO_COMPANY_ID` | `NFEIO_COMPANY_ID` | _(empty)_ | Default company id; per-call `company_id` overrides it. |
| `RABBITMQ_TOPOLOGY_INSTANCE` | `RABBITMQ_TOPOLOGY_INSTANCE` | `rabbitmq-topology-default` | Instance the reactor publishes through via `publish_message`. |

## Runtime environment variables

Read directly by the worker (`config.Load()` + `cmd/adapter/main.go`). Defaults
applied when unset.

| Env var | Required | Default | Read in | Purpose |
|---|:---:|---|---|---|
| `NFEIO_API_KEY` | **yes** | — | `config.go` | NFe.io API key. Fatal if empty. |
| `NFEIO_WEBHOOK_SECRET` | **yes** | - | `config.go` | Webhook HMAC secret. Fatal unless it is 32 to 64 characters without surrounding whitespace. |
| `NFEIO_BASE_URL` | no | `https://api.nfe.io` | `config.go` | NFe.io API base URL. |
| `NFEIO_COMPANY_ID` | no | _(empty)_ | `config.go` | Default company id; per-call override allowed. |
| `WEBHOOK_PORT` | no | `8082` | `config.go` | Legacy normalized-body listener port. Do not expose it to current NFe.io traffic. |
| `HEALTHCHECK_PORT` | no | `8080` | `config.go` | Health + `/metrics` port. |
| `TEMPLATES_DIR` | no | `manifest/templates` | `config.go` / `main.go` | Filesystem template dir; if present and readable it overrides the embedded templates baked into the binary. |
| `YGGDRASIL_TRANSPORT` | no | `http_json` | `main.go` | RPC transport: `http_json`/`http` (HTTP) or `amqp`/`rabbitmq` (AMQP). |
| `RPC_PORT` | no | `8081` | `main.go` | HTTP RPC port (`/rpc/describe`, `/rpc/execute`). HTTP transport only. |
| `BROKER_URL` | conditional | — | `main.go` | AMQP broker URL. **Required and fatal-if-empty only when `YGGDRASIL_TRANSPORT=amqp`.** Unused under HTTP. |
| `YGGDRASIL_CORE_URL` | no | `http://yggdrasil-core:9080` | `main.go` | Core endpoint the reactor's `publish_message` dispatcher POSTs to (`/api/v1/capabilities/invoke`). |
| `RABBITMQ_TOPOLOGY_INSTANCE` | no | `rabbitmq-topology-default` | `main.go` | `instance_ref` used by the publish dispatcher. |
| `YGGDRASIL_RUN_TOKEN` | no | _(empty)_ | `main.go` | Bearer token for the `publish_message` call to core. When empty, the call is sent without an `Authorization` header. |

## Ports

| Port | Server | Exposed | Notes |
|---|---|---|---|
| `8080` | health/metrics | `Dockerfile EXPOSE`, `deploy/service.yaml` (`health`) | `/healthz`, `/readyz`, `/metrics`. |
| `8081` | RPC (HTTP-JSON) | `Dockerfile EXPOSE`, `deploy/service.yaml` (`rpc`) | `/rpc/describe`, `/rpc/execute`. |
| `8082` | legacy normalized-body listener | `Dockerfile EXPOSE` | `/webhook/nfeio`. Deliberately absent from `deploy/service.yaml`; do not add provider ingress. |

## Transport

`YGGDRASIL_TRANSPORT` selects the RPC transport at startup:

- **`http_json`** (default, also `http` / empty) — serves `/rpc/describe` +
  `/rpc/execute` on `RPC_PORT`. `Describe()` advertises `transport: http_json` with
  those endpoints. This is the default the K8s Service and forward-drift auto-sync use.
- **`amqp`** (also `rabbitmq`) — consumes from queues
  `yggdrasil.adapter.nfeio.describe` / `yggdrasil.adapter.nfeio.execute`; requires
  `BROKER_URL`. The queues must already exist as durable quorum queues: SDK
  v0.9.1 checks existence passively and never creates fixed topology. Passive
  AMQP does not compare queue attributes, so rollout must verify quorum and
  durability through RabbitMQ management. `Describe()`
  then advertises `transport: rabbitmq` with those queues.

## Cross-references

- Per-capability input/output: [CAPABILITIES.md](./CAPABILITIES.md).
- Health, metrics, webhook runbook: [OPERATIONS.md](./OPERATIONS.md).
</content>
