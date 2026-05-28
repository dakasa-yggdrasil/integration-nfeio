# Claude Code Context: integration-template

> ## 🔐 READ FIRST: `INTEGRATION_CONTRACT.md`
>
> Before doing anything in this repo or in any `dakasa-yggdrasil/integration-*` adapter, read **[`INTEGRATION_CONTRACT.md`](./INTEGRATION_CONTRACT.md)**. That document is the canonical definition of what a Yggdrasil integration IS (and IS NOT), the four capability prefixes (`ensure_/observe_/destroy_/discover_`), the **Lego principle** (no cloud / secret-store / broker / DB coupling — Yggdrasil is provider-agnostic by design), and the forbidden anti-patterns. New adapters and new capabilities MUST conform — yggdrasil-core's schema validator enforces a subset at registration time.
>
> If you find yourself naming a capability `create_*`, `list_*`, `delete_*`, `update_*` for a resource operation — STOP and re-read §5 + §10 (self-test checklist).
>
> If you find yourself hardcoding "AWS" / "Vault" / "RabbitMQ" / "Postgres" in adapter code — STOP and re-read §2 (Lego principle).

Start with `AGENTS.md` for the rules-of-engagement summary. This file
expands the context for Claude-style assistants.

## What this repo is

A **production-ready scaffold** for writing a new Yggdrasil integration
adapter. Cloned by `yggdrasil new integration <name>` (the
`internal/scaffoldcli/` path in the `dakasa-yggdrasil/yggdrasil` CLI),
which rewrites the module path and integration name into the target
repo. Out of the box: `go test ./...` passes, the worker boots against
a local RabbitMQ, `/healthz` and `/readyz` are wired.

Repo: `github.com/dakasa-yggdrasil/integration-template` (open source,
Apache 2.0). Public since 2026-05-26 (Path A: flipped to public via
`update_repository_visibility` capability).

## Stack

- Go 1.25.
- `rabbitmq/amqp091-go v1.10.0` — direct AMQP usage in `main.go`
  (does NOT go through `yggdrasil-sdk-go` yet — the template
  currently ships its own minimal RPC layer in `controllers/message/`
  to keep adopters' import surface small).
- `go.uber.org/zap` — structured logging.

## Repo layout

```
main.go                        # AMQP connect, signal handler, /healthz + /readyz
controllers/message/           # AMQP RPC consumers (consume.go, describe.go, execute.go, rpc.go, register.go)
internal/adapter/              # spec.go — `Describe()` contract + `Execute()` switch; lint.go enforces it
internal/protocol/             # local RPC types (kept here, not imported from core, per AGENTS.md)
pkg/contractcheck/             # PUBLIC lint pkg: catches describe-contract drift in adapter specs
                               # — extracted (commit 95335d7) so integration-grafana and
                               # integration-secrets-management can reuse the same check
examples/                      # Sample run / wiring
cmd/                           # (extension point)
yggdrasil-quickstart.yaml      # Quickstart bundle so adopters can `yggdrasil install` this
templates/                     # Reserved
```

## Mandatory adapter contract

Every Yggdrasil integration adapter MUST:

1. Register under a **family** (the contract) and one or more
   **providers** (implementations).
2. Expose three mandatory operation categories: `describe`, `execute`,
   `health`.
3. Declare a credential schema + instance schema at the family or
   type manifest level (lives in `internal/adapter/spec.go`).
4. Keep `Describe()` in sync with what `Execute()` actually accepts.
   The `pkg/contractcheck` linter enforces this in CI; do NOT silence
   it.
5. Ship a `yggdrasil-quickstart.yaml` so adopters can install with
   `yggdrasil install dakasa-org/integration-your-thing`.

## Runtime expectations

- Worker connects to RabbitMQ via `BROKER_URL` (no default — fatal if
  unset).
- `/healthz` is liveness only (always 200).
- `/readyz` reflects RabbitMQ connection state (503 when closed).
- Graceful shutdown on `SIGINT`/`SIGTERM`. The main loop also exits
  when `conn.NotifyClose()` fires — kubelet then restarts the pod
  (cleanest path; matches the pattern in `yggdrasil-core` commit
  `9d30e34`).
- Env knobs: `HEALTHCHECK_PORT` (default `8080`),
  `HTTP_READ_HEADER_TIMEOUT_SECONDS`, `HTTP_READ_TIMEOUT_SECONDS`,
  `HTTP_WRITE_TIMEOUT_SECONDS`, `HTTP_IDLE_TIMEOUT_SECONDS`.

## CI / image flow

`.github/workflows/`:

- `ci.yml` — go test + lint + contractcheck.
- `release.yml` — publishes the worker image to
  `ghcr.io/dakasa-yggdrasil/integration-template`.
- `publish-oci.yml` — publishes the `yggdrasil-quickstart.yaml` as an
  OCI artifact on tag (commit `2f47f0e`). The `yggdrasil install`
  CLI consumes that with the `oci://` ref support added in
  `dakasa-yggdrasil/yggdrasil` commit `6da5dfe`.
- `emit-deploy-event.yml` — POSTs the deploy event into yggdrasil-core
  (same soft-skip pattern as everywhere else).
- `deploy.yml` — placeholder; this repo is a template, not a service.
- `incident-escalation.yml` + `postmortem.yml` — Heimdall-driven ops
  automation.

Cross-org private action note: workflows that previously used
`dakasa-yggdrasil/action-emit-workflow-run` should use **inline
curl+jq** (see `~/.claude/projects/-Users-dakasa-projects/memory/reference_inline_curl_jq_cross_org_actions.md`).
Now that this repo is public the cross-org constraint is relaxed, but
the inline pattern stays the safer default.

## Recent commits

```
95335d7 ✨ contractcheck: extract describe-contract lint to public pkg   ← now consumed by integration-grafana + integration-secrets-management
fe2b853 ✨ lint: catch describe contract drift in adapter spec
2f47f0e ✨ Add publish-oci GHA workflow: publish quickstart as OCI artifact on tag
1d32ccb ✨ Production-ready scaffold: yggdrasil-quickstart stub + release workflow
1fa8558 ✨ Public-ready: Apache 2.0 LICENSE + adopter-facing README
1aa2a97 Document scheduling failure guardian signals
7854402 Add incident escalation workflows
6f1648f Document Heimdall lightweight support contract
f10535a Accept remediation workflow inputs
6fc3310 Use official workflow run action
ae62b20 Add dogfood deploy workflows
d241b97 Wait for broker in standalone and monorepo runtime
ee7b42e Split monorepo and standalone Compose setups
71f6a5d Align repository naming with integration-*
281503e Initialize repository with production pack and AI context
```

## Validation

```bash
go test ./...
task config
task build:image
task up         # local stack via compose
task down
```

## Mandatory rules (from AGENTS.md, restated)

- **Keep the plugin standalone.** Do not import runtime/domain code
  from `yggdrasil-core` or the `yggdrasil` monorepo. Protocol types
  stay local to this repo.
- **`describe` MUST stay aligned with `execute`.** `pkg/contractcheck`
  catches drift; don't silence it.
- **Rename/add capabilities → update tests, examples, and README in
  the same change.**
- **Fail fast over silent degradation.** No swallowing AMQP errors,
  no silent NACK loops.
- **Business authority stays in `yggdrasil-core`.** This worker owns
  integration runtime behavior only.

## Where things live

- Adapter spec (`Describe`/`Execute`) → `internal/adapter/spec.go`
- Contract-drift lint → `internal/adapter/lint.go` + `pkg/contractcheck/`
- AMQP consume / publish plumbing → `controllers/message/`
- Health server → `main.go`
- Quickstart for `yggdrasil install` → `yggdrasil-quickstart.yaml`
