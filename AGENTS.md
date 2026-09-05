# AGENTS

## What this repo is

`integration-nfeio` — a standalone Yggdrasil integration worker
(`integration_type: nfeio`, namespace `global`). It wraps the **NFe.io v2 REST
API** to run the lifecycle of Brazilian fiscal documents (NFSe service
invoices, companies, webhook subscriptions) and runs an **HMAC-verified webhook
listener** that republishes NFe.io status callbacks to `enterprise-payments.*`.

`domain: payments`. Repo: `github.com/dakasa-yggdrasil/integration-nfeio`.

## Source of truth

The adapter's real contract is whatever **`Describe()` returns** in
`providers/nfeio/adapter/spec.go`. Read it before changing anything; it owns the
capability list, resource types, credential/instance schemas, transport, and
version (`AdapterVersion = "3.1.0"`). `CLAUDE.md` has the full map. If a doc
disagrees with `spec.go`, `spec.go` wins.

## Capabilities

14 callable `execute` ops + 1 webhook reactor (`nfse_webhook_received`, NOT an
execute op). Canonical names follow the `ensure_/observe_/destroy_/discover_`
convention; `retrieve_pdf`, `retrieve_xml`, `manage_template`, `bulk_issue`,
`calculate_iss` are allowlisted helpers/actions. Pre-v2.0.0 names (`issue_nfse`,
`get_nfse_status`, `cancel_nfse`, `register_company`, `list_municipalities`, etc.)
were removed at v3.0.0. Don't add new `create_/list_/delete_/update_` names.

## Non-negotiable rules

- Keep `Describe()` aligned with what `Execute()` accepts.
  `cmd/lint-action-catalog` (`pkg/contractcheck`) gates this in CI — don't
  silence it. Each catalog entry has a matching `manifest/capability.*.yaml`.
- Keep the worker standalone: no imports of yggdrasil-core / monorepo runtime.
  Use `yggdrasil-sdk-go` + the local `family/contract` types only.
- Add/rename a capability → update `spec.go`, the matching
  `manifest/capability.*.yaml`, tests, `docs/`, and the README in one change.
- Fail fast over silent degradation. 404 → already-absent success is
  deliberate; terminal errors (e.g. 422 `cancellation_window_closed`) stay
  failures.
- This worker owns integration runtime behavior only. Business authority stays
  in yggdrasil-core.

## Transport / runtime

- Default transport `http_json` — RPC on `:8081` (`RPC_PORT`),
  `/rpc/describe` + `/rpc/execute`. AMQP is opt-in via
  `YGGDRASIL_TRANSPORT=amqp` + `BROKER_URL` (queues
  `yggdrasil.adapter.nfeio.{describe,execute}`).
- Health on `:8080` (`/healthz`, `/readyz`, `/metrics`); webhook on `:8082`
  (`/webhook/nfeio`).
- Required creds: `NFEIO_API_KEY`, `NFEIO_WEBHOOK_SECRET` (`config.Load()` exits
  if either is empty).

## Manifest synchronization

`manifest/integration_type.nfeio.yaml` is a static snapshot derived from
`Describe()`. `Describe()` is authoritative. Do not change code to match a stale
manifest; re-derive the manifest when the live contract changes, and do not edit
it as part of unrelated work.

## Commands

```bash
go test ./...
go run ./cmd/validate-templates manifest/templates
go run ./cmd/lint-action-catalog
docker build --build-arg VERSION=$(git rev-parse --short HEAD) .
```
