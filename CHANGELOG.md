# Changelog

## v2.0.0 — 2026-05-27

Universal capability naming convention adoption (per
`docs/superpowers/specs/2026-05-27-yggdrasil-integration-capability-convention.md`
Tier C nfe.io). **Breaking** — every CRUD-style capability name is renamed
to the canonical `ensure_/observe_/destroy_` triple; pre-v2.0.0 names are
accepted for one minor cycle via the SDK `WithLegacyNames` shim (removed
in v3.0.0).

### Renamed

| Pre-v2.0.0           | v2.0.0 canonical                |
|----------------------|---------------------------------|
| `issue_nfse`         | `ensure_service_invoice`        |
| `get_nfse_status`    | `observe_service_invoices` (filter `{id}`) |
| `cancel_nfse`        | `destroy_service_invoice`       |
| `register_company`   | `ensure_company`                |
| `list_municipalities`| `observe_municipalities`        |

### Added (NEW)

- `observe_service_invoices` — pagination + single-resource lookup
- `observe_companies` — read companies registered at NFe.io
- `webhook_subscription` resource type (NEW) with canonical triple:
  - `ensure_webhook_subscription` — POST /v2/webhooks; 409/422 adopts
  - `observe_webhook_subscriptions` — list / single-by-id
  - `destroy_webhook_subscription` — DELETE; 404 → success per convention §5

### Kept (allowlisted helpers, see CLAUDE.md)

- `retrieve_pdf` / `retrieve_xml` — file URL retrieval (action-shaped)
- `manage_template` — control-plane action, not external resource
- `bulk_issue` — bulk action, documented special case
- `calculate_iss` — pure-function helper

### SDK

- Bump: `yggdrasil-sdk-go v0.4.0 → v0.5.0` (consumes `sdk/reconcile`).
- `reconcilers.go` exposes `serviceInvoiceReconciler`, `companyReconciler`,
  and `webhookSubReconciler` implementing the SDK `Reconciler[D, O]`
  interface with `WithLegacyNames` shims documented in code.

### Compat

- Hand-written switch in `executeRoute` accepts legacy names
  (`issue_nfse`, `get_nfse_status`, `cancel_nfse`, `register_company`,
  `list_municipalities`) and routes to canonical handlers transparently.
- Manifest YAMLs (`manifest/capability.*.yaml`) renamed to canonical
  names; pre-v2.0.0 capability YAMLs deleted.

### Removed

- `Op*` constants for legacy names: `OpIssueNfse`, `OpGetNfseStatus`,
  `OpCancelNfse`, `OpRegisterCompany`, `OpListMunicipalities` (call sites
  use canonical `OpEnsureServiceInvoice`, etc.).

## v1.0.0 — 2026-05-26

Initial release.

- 10 capabilities (`issue_nfse`, `get_nfse_status`, `cancel_nfse`, `retrieve_pdf`,
  `retrieve_xml`, `register_company`, `list_municipalities`, `manage_template`,
  `bulk_issue`, `calculate_iss`)
- Reactor `nfse_webhook_received` (HMAC verify + LRU dedup + RabbitMQ publish
  via `publish_message` on the rabbitmq-topology instance)
- 5 município templates (São Paulo, Rio de Janeiro, Curitiba, Florianópolis,
  Belo Horizonte)
- SDK pin: yggdrasil-sdk-go v0.4.0 (uses `sdk/webhookhttp` for HMAC-SHA256
  signature verification)
- 7 Prometheus metrics on `/metrics` + OTel span per HTTP call
- Multi-arch (amd64 + arm64), distroless/base-debian12:nonroot
