# GEMINI

`integration-nfeio` — standalone Yggdrasil integration worker
(`integration_type: nfeio`, namespace `global`, `domain: payments`). Wraps the
**NFe.io v2 REST API** for Brazilian fiscal documents (NFSe service invoices,
companies, webhook subscriptions) and runs an **HMAC-verified webhook listener**
that republishes NFe.io status callbacks to `enterprise-payments.*`.

Repo: `github.com/dakasa-yggdrasil/integration-nfeio`.

## Read first

The real contract is whatever **`Describe()` returns** in
`providers/nfeio/adapter/spec.go` — capabilities, resource types,
credential/instance schemas, transport, version. Trust it over any doc. Then
read `AGENTS.md` (rules) and `CLAUDE.md` (full map).

## Quick facts

- **Version:** `AdapterVersion = "3.1.2"` (`spec.go`).
- **Transport:** default `http_json`, RPC on `:8081` (`/rpc/describe` +
  `/rpc/execute`); AMQP opt-in via `YGGDRASIL_TRANSPORT=amqp` + `BROKER_URL`
  (queues `yggdrasil.adapter.nfeio.{describe,execute}`). Health `:8080`,
  webhook `:8082`.
- **Capabilities:** 14 callable `execute` ops + 1 reactor
  (`nfse_webhook_received`, webhook-triggered, not an execute op). Canonical
  `ensure_/observe_/destroy_` triples for service_invoice / company /
  webhook_subscription, plus `observe_companies`, `observe_municipalities`, and
  allowlisted helpers `retrieve_pdf`, `retrieve_xml`, `manage_template`,
  `bulk_issue`, `calculate_iss`. Pre-v2.0.0 aliases were removed at v3.0.0.
- **Credentials (required):** canonical contract keys `nfeio_api_key` and
  `nfeio_webhook_secret`, bound at runtime to `NFEIO_API_KEY` and
  `NFEIO_WEBHOOK_SECRET`.

## Rules

- Keep `Describe()` aligned with `Execute()`; `cmd/lint-action-catalog`
  (`pkg/contractcheck`) gates it in CI.
- Keep the worker standalone (no yggdrasil-core / monorepo imports; use
  `yggdrasil-sdk-go` + local `family/contract`).
- Add/rename a capability → update `spec.go`, `manifest/capability.*.yaml`,
  tests, `docs/`, README together. Don't add `create_/list_/delete_/update_`
  names; use the `ensure_/observe_/destroy_/discover_` convention.
- `manifest/integration_type.nfeio.yaml` is a stale snapshot (says `2.0.0`);
  `spec.go` is authoritative — don't change code to match the manifest.
