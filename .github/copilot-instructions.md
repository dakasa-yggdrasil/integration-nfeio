# GitHub Copilot — Yggdrasil integration repository

READ FIRST: `INTEGRATION_CONTRACT.md` in the repo root. It is the canonical law defining what a Yggdrasil integration IS / IS NOT, the four canonical capability prefixes, the Lego principle, and the forbidden anti-patterns.

## ABSOLUTE rule #0 — Yggdrasil scope vs Backend scope

Yggdrasil = IDP for the operating COMPANY's own internal resources (Stripe webhook URL config, AWS infra buckets, GitHub repo provisioning, secret management — company concerns). Backend = end-user-facing business operations (charge user, refund order, emit user's NFSe — end-user concerns).

Heuristic: "If the company changes ownership / sells / shuts down, does the resource follow the COMPANY or the END-USER?" Company → Yggdrasil. End-user → backend.

When suggesting a new capability or refactor, ALWAYS first determine which side of the line it's on. Suggesting that a Stripe charge for an end-user go through `integration-stripe` is wrong (that's backend). Suggesting that Stripe webhook URL provisioning go through backend code is wrong (that's Yggdrasil). See contract §0 for the full table.

## Hard rules (excerpt — full text in `INTEGRATION_CONTRACT.md`)

- Resource ops use the canonical prefixes: `ensure_<resource>` / `observe_<resource_type>` / `destroy_<resource>` / `discover_<resource_type>`. NEVER suggest `create_*`, `list_*`, `delete_*`, `update_*` for resource operations.
- Lego principle: NEVER hardcode AWS, GCP, Vault, RabbitMQ, Postgres, or any specific cloud / secret store / broker / DB. Suggest abstractions via `credentials_ref` URI scheme or capabilities from other integrations.
- Integration is infrastructure reconciliation, NOT business logic. Don't suggest code that processes inbound webhook payloads as business events, stores business state, or makes business decisions.
- Idempotent by contract: every mutation handler must be safe to retry; `ensure_*` adopts pre-existing resources via GET-then-PUT.
- NEVER suggest logging credentials, secrets, signing keys, or refresh tokens.

## Code generation guidance

When suggesting a new capability:
1. Determine if it represents a resource (stable external identity) or an action (one-shot side effect).
2. Resource → use `ensure_/observe_/destroy_`. Action → use a name on the contract's allowlist (§5).
3. The handler is idempotent — GET-then-PUT to adopt existing resources.
4. Credentials come from `credentials_ref`; never inline.
5. Add the capability to both `action_catalog` AND the appropriate `resource_types[].default_actions` in the manifest.

When suggesting wire-protocol code:
- Match the existing `Execute` switch pattern in the adapter.
- Update the `Describe` catalog in the same pass.
- Add tests using `httptest` mock servers (no real provider calls in unit tests).

If your suggestion violates any rule above, the suggestion is wrong — restructure it.

## Repository scaffold rules

- Keep adapter protocol types local to this repo.
- Do not import internal code from the Yggdrasil monorepo.
- Keep `describe` and `execute` aligned.
- Update tests and examples with every capability change.
- Prefer explicit env vars and predictable startup/shutdown behavior.
