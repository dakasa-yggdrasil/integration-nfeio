# GEMINI

## 🔐 READ FIRST: `INTEGRATION_CONTRACT.md`

Before any change in this repo or any adapter cloned from it, read [`INTEGRATION_CONTRACT.md`](./INTEGRATION_CONTRACT.md). It defines:
- **§0 ABSOLUTE: Yggdrasil scope vs Backend scope** — Yggdrasil = IDP for COMPANY's internal resources (webhook URL config, infra buckets, repo provisioning). Backend = END-USER business (charge user, refund order). Heuristic: resource follows company on ownership change → Yggdrasil; follows end-user → backend.
- What a Yggdrasil integration IS / IS NOT
- The four canonical capability prefixes (`ensure_/observe_/destroy_/discover_`)
- **Lego principle** (no cloud/secret-store/broker/DB hardcoding)
- **§6.5 mandatory mutation event emission** (golden rule)
- Forbidden anti-patterns

If you find yourself naming a capability `create_*`, `list_*`, `delete_*`, `update_*` for a resource — STOP and re-read §5 + §10.
If you hardcode AWS / Vault / RabbitMQ / Postgres — STOP and re-read §2.
If you're designing a capability to handle end-user business (charge, refund, subscribe) — STOP and re-read §0. That's backend territory.

Then read `AGENTS.md` for repo-specific rules.

Focus areas:
- Keep this repository transport/runtime focused.
- Keep protocol types local.
- Validate any capability change against README, tests, and examples.
