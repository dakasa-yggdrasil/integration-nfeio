# AGENTS

## 🔐 READ FIRST: `INTEGRATION_CONTRACT.md`

Every AI assistant making changes in this repo or in ANY adapter cloned from this template MUST read [`INTEGRATION_CONTRACT.md`](./INTEGRATION_CONTRACT.md) first. It defines what a Yggdrasil integration IS (and IS NOT), the canonical capability prefixes (`ensure_/observe_/destroy_/discover_`), the **Lego principle** (provider-agnostic — no hardcoded AWS / Vault / RabbitMQ / Postgres), and the forbidden anti-patterns. New capabilities MUST conform; non-conformant names will be flagged by yggdrasil-core's schema validator at registration (warn-only Phase 1, hard-fail Phase 2).

If a user request would lead to a `create_*` / `list_*` / `delete_*` capability for a resource operation, or to hardcoding a specific cloud/secret-store/broker — STOP and revisit the contract before writing code.

## Repo role
This repository is a standalone Yggdrasil integration worker. It exposes an honest adapter contract through `describe` and executes capabilities through `execute`.

## Non-negotiable rules
- Keep the plugin standalone. Do not import runtime/domain code from the Yggdrasil monorepo.
- Keep protocol types local to this repository.
- `describe` must stay aligned with what `execute` actually accepts.
- If you add or rename capabilities, update tests, examples, and README in the same change.
- Prefer failing fast over silently degrading adapter behavior.
- This worker owns integration runtime behavior only. Business authority stays in `yggdrasil-core`.

## Runtime expectations
- The worker connects to RabbitMQ through `BROKER_URL`.
- `/healthz` is liveness only.
- `/readyz` must reflect whether the worker is still connected to RabbitMQ.
- Production changes should preserve graceful shutdown on `SIGINT`/`SIGTERM`.

## Commands
- `go test ./...`
- `task config`
- `task build:image`
- `task up`
- `task down`

## Change checklist
- Update adapter tests before claiming a new capability works.
- Keep examples under `examples/` aligned with the adapter contract.
- Prefer explicit env vars and documented defaults.
- Do not add Yggdrasil-core-specific data models here; use the public contract only.
