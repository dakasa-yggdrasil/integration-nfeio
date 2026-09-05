# Development — integration-nfeio

Build, test, the describe/execute contract, and repo layout. Sourced from
`go.mod`, `.github/workflows/{ci,release}.yml`, `Dockerfile`, and the adapter source.
Back to the [README](../README.md) · contract authority:
[`yggdrasil-core`](https://github.com/dakasa-yggdrasil/yggdrasil-core).

---

## Prerequisites

- Go **1.25** (`go.mod` declares `go 1.25.0`).
- Docker (for the image build).
- `yggdrasil-sdk-go` **v0.8.3** is the SDK pin.

> This repo has **no `Taskfile.yml`**. The `task ...` commands referenced in the older
> `AGENTS.md` / `CLAUDE.md` do not apply here — use the `go` / `docker` commands below.

## Build & test

```bash
go test ./...                                       # unit tests
go run ./cmd/lint-action-catalog                    # describe/execute/catalog drift gate
go run ./cmd/validate-templates ./manifest/templates/   # validate bundled templates
go vet ./...
docker build -t integration-nfeio:dev .             # multi-stage distroless image
```

These mirror the CI gate (`.github/workflows/ci.yml`), which runs exactly:
`lint-action-catalog` → `validate-templates` → `go vet` → `go test ./... -count=1` →
`docker build`.

Run the worker locally (HTTP transport is the default):

```bash
NFEIO_API_KEY=sk_test_xxx \
NFEIO_WEBHOOK_SECRET=whsec_xxx \
TEMPLATES_DIR=manifest/templates \
go run ./cmd/adapter
```

## Repo layout

```
cmd/
  adapter/                 # main binary: boots 3 listeners (RPC, health, webhook)
    main.go                # transport select, health server, webhook wiring, signal handler
    templates/*.yaml       # templates copied here at build time for go:embed (5 municípios)
  lint-action-catalog/     # CI gate: SupportedExecuteOperations vs Describe() vs ActionCatalog
  validate-templates/      # CI gate: validates manifest/templates/*.yaml
family/contract/           # wire-shape JSON types exchanged with yggdrasil-core (types.go)
pkg/contractcheck/         # PUBLIC describe-contract lint pkg (reused across adapters)
providers/nfeio/
  config/                  # config.Load() — env parsing + mandatory-secret checks
  adapter/                 # the provider implementation:
    spec.go                #   Describe() contract, Op* constants, Prometheus metrics
    adapter.go, client.go  #   NFe.io HTTP client + IssueNFSe / company / observe ops
    reconcilers.go         #   SDK reconcile.Reconciler wiring (ensure/observe/destroy)
    bulk_issue.go          #   bulk_issue (cap 50, semaphore 5, partial-failure)
    tax_calc.go            #   calculate_iss (pure function)
    manage_template.go, template_loader.go, list_municipalities.go
    webhook_server.go      #   inbound webhook listener + reactor pipeline
    publish_dispatch.go    #   publish_message dispatcher → yggdrasil-core
    bearer.go, retrieve_*, register_company, *_test.go
manifest/
  integration_type.nfeio.yaml   # the integration type (provider, schemas, action catalog)
  instance.nfeio.yaml           # sample instance binding
  capability.*.yaml             # per-capability input schemas (14 + reactor)
  reactor.nfse_webhook_received.yaml
  templates/*.yaml              # 5 município templates
deploy/service.yaml             # source-of-truth K8s Service (ports 8080 + 8081)
Dockerfile                      # golang:1.25-bookworm -> distroless/base-debian12:nonroot
.github/workflows/              # ci.yml, release.yml, emit-deploy-event.yml
```

## The describe / execute contract

Every Yggdrasil adapter exposes two mandatory operations:

- **`describe`** (`/rpc/describe`) — returns the adapter's contract:
  provider, transport, credential/instance schemas, resource types, the action
  catalog, normalization, and execution metadata. `yggdrasil-core` calls this to
  verify the live adapter matches the stored `integration_type` manifest. Built by
  `Describe()` in `providers/nfeio/adapter/spec.go`.
- **`execute`** (`/rpc/execute`) — runs one capability. Inbound envelopes route
  through the SDK `reconcile.Dispatch` table first (for the
  `ensure_/observe_/destroy_` triples, which also auto-emit mutation events to core),
  then fall back to the hand-written `executeRoute` switch for the helpers
  (`retrieve_pdf`, `retrieve_xml`, `manage_template`, `bulk_issue`, `calculate_iss`,
  `observe_municipalities`). Legacy aliases were removed at v3.0.0.

The reactor `nfse_webhook_received` is **not** part of `execute` — it is triggered by
the inbound webhook HTTP server.

### Keeping describe ⇄ execute aligned

`SupportedExecuteOperations`, `Describe().ActionCatalog`, and the per-capability
manifests must stay in sync. The `cmd/lint-action-catalog` gate (also exposed as the
public `pkg/contractcheck` package, reused by other adapters) catches drift in CI —
do not silence it. When you add or rename a capability, update the manifest YAML,
the `Op*` constant + `SupportedExecuteOperations`, `actionCatalog()`, tests, and these
docs in the same change.

## Image & release

- `Dockerfile`: multi-stage `golang:1.25-bookworm` → `distroless/base-debian12:nonroot`.
  Bundled templates are copied to `/etc/nfeio/templates`. `EXPOSE 8080 8081 8082`.
- `release.yml` publishes `ghcr.io/dakasa-yggdrasil/integration-nfeio` (multi-arch
  amd64+arm64) on push to `main` (`edge` / `branch-main-latest` / `sha-<short>`), on
  tag push (`v<version>` / `latest`), and on manual `workflow_dispatch`.
- The adapter version is `AdapterVersion` in `spec.go` and is overridable at link time
  via `-ldflags "-X main.Version=vX.Y.Z"`.

## Conventions (from INTEGRATION_CONTRACT.md)

- **Standalone.** No runtime/domain imports from `yggdrasil-core` or the monorepo;
  wire types stay local (`family/contract`).
- **Lego principle.** No hardcoded cloud / secret-store / broker / DB. The reactor's
  broker target is reached generically through the `publish_message` capability on a
  named instance — not a hardcoded RabbitMQ client.
- **Canonical capability names.** Use `ensure_/observe_/destroy_` for resource
  operations; helper/bulk/pure-function exceptions (`retrieve_*`, `bulk_issue`,
  `manage_template`, `calculate_iss`) are explicitly allowlisted.
- **Fail fast.** Mandatory config missing is fatal at boot; no silent degradation.

## Compatibility matrix

| Component | Pin |
|---|---|
| Go | 1.25 |
| `yggdrasil-sdk-go` | v0.8.3 |
| Adapter version (`Describe()`) | 3.1.0 |
| Transports | `http_json` (default), `amqp` |
</content>
