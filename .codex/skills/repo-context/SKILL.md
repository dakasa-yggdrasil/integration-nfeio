# Repo Context

Use this context whenever working in this repository.

## ⚠️ READ FIRST: `INTEGRATION_CONTRACT.md`

Before any change here or in any adapter cloned from this template, read `INTEGRATION_CONTRACT.md` in the repo root. It is the canonical law defining:
- §0 Yggdrasil scope vs Backend scope (ABSOLUTE rule — see below)
- What a Yggdrasil integration IS / IS NOT
- The four canonical capability prefixes (`ensure_/observe_/destroy_/discover_`)
- The **Lego principle**: no cloud / secret-store / broker / DB hardcoding — Yggdrasil is provider-agnostic by design
- §6.5 mandatory mutation event emission (golden rule)
- Forbidden anti-patterns

## ABSOLUTE rule #0 — Yggdrasil scope vs Backend scope

Yggdrasil = IDP for the operating COMPANY's own internal resources (webhook URL config, infra buckets, repo provisioning). Backend = end-user-facing business operations (charge user, refund order). Heuristic: if resource follows company on ownership change → Yggdrasil; follows end-user → backend.

Example: provisioning Stripe webhook URL = YGGDRASIL (integration-stripe ensure_webhook_endpoint). Charging end-user = BACKEND (enterprise-payments-api direct Stripe call). Same provider, opposite sides.

Hard rule reminders:
- Resource ops use `ensure_<resource>` / `observe_<resource_type>` / `destroy_<resource>`. NEVER `create_*`, `list_*`, `delete_*`, `update_*` for resources.
- Integration is infra reconciliation, NOT business logic.
- Adoption-aware: ensure_* must adopt existing resources gracefully.
- Idempotent by contract.
- NEVER log credentials, secrets, signing keys.

If a planned change violates any of the above, STOP and re-read the contract.

## Identity
This is a standalone Yggdrasil integration worker repository.

## What good changes look like
- Adapter contract remains honest.
- README, tests, and examples move together.
- Startup is explicit and production-safe.
- Runtime behavior is isolated from Yggdrasil-core internals.
- Capability names follow the convention (`ensure_/observe_/destroy_/discover_/on_`).
- Lego principle preserved: no hardcoded cloud, secret store, broker, or DB.

## Default workflow
1. Read `INTEGRATION_CONTRACT.md` if you have not in this session.
2. Inspect `internal/adapter` and `controllers/message` before editing.
3. Update tests in the same pass as capability changes.
4. Run `go test ./...`.
5. If runtime/bootstrap changed, run `task config` and `task build:image`.
