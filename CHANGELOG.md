# Changelog

## v3.1.0 - 2026-09-05

### Security

- Extended exact-ID webhook reconciliation with an atomic, explicitly
  confirmed migration that sets the provider HMAC from the adapter's existing
  runtime credential and removes every case variant of the legacy static
  `Authorization` callback header.
- Partial or mismatched migration requests fail before any provider call. The
  runtime HMAC must satisfy the provider's 32 to 64 character contract, never
  enters capability input, and remains absent from outputs, mutation events,
  errors, and logs.
- Ordinary reconciliation remains backward compatible and changes only
  `insecureSsl=true` to `false`. The migration still targets one pre-existing
  provider ID and cannot create, list, discover, or delete a webhook.

## v3.0.0 - 2026-09-05

### Security

- Replaced create-shaped webhook reconciliation with exact-ID observation and
  update. `ensure_webhook_subscription` now requires an existing provider `id`
  plus `insecure_ssl=false`, performs GET then optional PUT on
  `/v2/webhooks/{id}`, and never POSTs, lists, or adopts by URI or filters.
- The only permitted webhook mutation is provider field `insecureSsl=true` to
  `false`. The PUT body starts from the complete observed `webHook` object and
  preserves documented, secret-bearing, and unknown future fields unchanged.
  A second exact-ID GET confirms the secure setting after the PUT.
- Webhook resource, adoption, and mutation-event outputs are reduced to safe
  metadata. Provider `secret`, URI, headers, properties, raw bodies, and echoed
  error content are excluded from responses, events, errors, and logs.
- `destroy_webhook_subscription` now requires `confirm_id` to exactly equal the
  target `id`. It is no longer a webhook default action, and the generic SDK
  destroy path refuses to call NFe.io without the full confirmation payload.

### Breaking

- `ensure_webhook_subscription` input changed from URL/events creation intent to
  exact `id` plus `insecure_ssl=false` reconciliation intent.
- `observe_webhook_subscriptions` requires exact `id`; enumeration and cursor
  input are no longer supported.
- The v2 compatibility aliases were removed as scheduled by the integration
  contract's one-minor-cycle rule.

## Unreleased — 2026-05-27

### Changed

- **`.github/workflows/release.yml`**: build image on every push to
  `main` (matches the integration-efi pattern). Previously the
  workflow only triggered on tag push, forcing every cycle to either
  bump a tag OR manually dispatch the workflow. Tag-push + manual
  dispatch triggers remain in place; the additional main-branch
  trigger removes the friction so adapter rolls stay declarative.
- Tagging strategy aligned with integration-efi via
  `docker/metadata-action@v5`:
  - `branch-main-latest` + `sha-<short>` + `edge` on main pushes
  - `v<version>` + `latest` on tag push
  - optional `${{ inputs.tag }}` on workflow_dispatch
- **`deploy/service.yaml`**: `metadata.namespace` corrected
  `yggdrasil-adapters` → `dakasa` (the cluster namespace where the
  Service is actually live, verified 2026-05-27 via `kubectl get svc
  -A | grep integration-nfeio`). Pre-fix the source manifest would
  have created a duplicate Service in the wrong namespace on the next
  `apply_manifest` cycle.

## v2.2.3 — 2026-05-27

### Fixed

- **`Describe().CredentialSchema.Mode`** drift: was `"env"`, must be
  `"inline"`. Yggdrasil core's Phase 1 manifest validator only
  accepts `inline` (or `none`); `env` triggered a registration
  rejection that forced a manual hand-patch on every re-register
  (caught by the SDK v0.8.0 Phase C smoke). The manifest YAML
  (`manifest/integration_type.nfeio.yaml`) already declared
  `mode: inline` — only the source `spec.go` was drifted, so the
  spec.go ↔ YAML mismatch could survive a clean rebuild. New
  `TestSpec_CredentialSchemaModeIsInline` locks the wire-advertised
  value to prevent regression.
- Note: the `Required` array (`NFEIO_API_KEY`,
  `NFEIO_WEBHOOK_SECRET`) is preserved verbatim — the SCHEMA mode
  switches; the required-key list is unchanged. Downstream tooling
  that binds env vars from secret-store entries reads `Required`
  identically across `inline` and `env` modes.
- **K8s Service rpc port missing** — added explicit
  `deploy/service.yaml` declaring two named ports (8080 health, 8081
  rpc). Pre-2.2.3 the live Service only exposed 8080, so
  yggdrasil-core forward-drift auto-sync hit "connection refused"
  reaching `/rpc/describe` via service DNS. The Phase C agent worked
  around this with `kubectl exec deploy/yggdrasil -- wget` against
  the pod IP — not a fix. Apply the manifest via `apply_manifest`
  workflow on next deploy. Container ports 8080 + 8081 are already
  what `cmd/adapter/main.go` binds to (RPC_PORT default 8081,
  HealthPort default 8080); this manifest aligns Service routing.

### Changed

- **Bumped `yggdrasil-sdk-go` v0.8.0 → v0.8.1**. The SDK patch closes
  the destroy resource_id inference gap so `nfeio.<resource>.destroyed`
  events land in `event_log` with the correct identifier (was
  silently dropped with HTTP 400 from yggdrasil-core).

## v2.2.1 — 2026-05-27

### Changed

- **Bumped `yggdrasil-sdk-go` v0.7.0 → v0.8.0**. SDK ships an opt-in
  `DestroyWithDesired[D]` interface that closes the latent
  destroy-credential bug for adapters that load credentials per-request
  through reserved bridge keys (`__instance_credentials` etc).
- **No reconciler changes required.** integration-nfeio binds the
  NFe.io HTTP client at `WireReconcilers` time via the singleton
  `cli *Client` — destroy_* operations don't load credentials from
  reserved bridge keys, so the SDK v0.8.0 opt-in interface is not
  needed here. The version bump keeps the SDK pin aligned across the
  ecosystem.

## v2.2.0 — 2026-05-27

### Changed

- Bump yggdrasil-sdk-go v0.6.0 → v0.7.0 to pick up the public
  `reconcile.Dispatch` API.
- Production runtime migrated to `reconcile.Dispatch`.
  `cmd/adapter/main.go` calls `WireReconcilersWithInstance(a, cli,
  templates, "")` BEFORE the `Register("execute", ...)` chain;
  `ExecuteHandler` routes inbound envelopes through
  `reconcile.Dispatch` first and falls back to the legacy
  `executeRoute` switch only for ops outside the
  ensure_/observe_/destroy_ triples — retrieve_pdf, retrieve_xml,
  manage_template, bulk_issue, calculate_iss, observe_municipalities
  (cache-backed), and the pre-v2.0.0 legacy aliases (issue_nfse,
  get_nfse_status, cancel_nfse, register_company, list_municipalities).
- §6.5 mutation event auto-emission is now LIVE for production
  traffic (previously TEST-ONLY) when `YGGDRASIL_CORE_URL`
  + `YGGDRASIL_RUN_TOKEN` are wired in the cluster manifest.
- `ExecuteHandler` signature now accepts `*sdkadapter.Adapter`
  alongside the logger / client / templates / deps.

## v2.1.0 — 2026-05-27

### Added

- Bump yggdrasil-sdk-go v0.5.0 → v0.6.0 to pick up the additive
  `sdk/events` package + the new `reconcile.WithEmitter` /
  `WithProvider` / `WithInstanceID` options.
- `WireReconcilers` now wires a §6.5 mutation-event emitter via
  `events.NewHTTPEmitter()` when `YGGDRASIL_CORE_URL` is set; degrades
  to `events.NoopEmitter{}` otherwise. Successful `Ensure()` /
  `Destroy()` invocations auto-emit `nfeio.<resource>.ensured` /
  `nfeio.<resource>.destroyed` events to yggdrasil-core
  `POST /api/v1/events` per INTEGRATION_CONTRACT.md §6.5.
- New `WireReconcilersWithInstance` variant accepting an instanceID
  so multi-tenant `MutationEvent.InstanceID` is preserved; the
  legacy single-arg `WireReconcilers` keeps signature stability by
  delegating with `instanceID=""`.
- Emitter is env-driven (`YGGDRASIL_CORE_URL` + `YGGDRASIL_RUN_TOKEN`)
  so the adapter stays Lego-compliant — no broker / secret-store /
  cloud is hardcoded.

### Notes

- Production `cmd/adapter/main.go` continues to use the hand-written
  `executeRoute` switch as the runtime dispatch path; emission
  activates when the runtime moves onto SDK reconcile dispatch in a
  follow-up cycle (SDK v0.6.0 ships the composable-handler primitive
  noted in the v2.0.0 changelog).
- Best-effort emission: failed posts log WARN but do not fail the
  capability call.

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
