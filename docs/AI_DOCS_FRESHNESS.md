# AI docs freshness stamp

Records the commit an AI (or agent-assisted human) last reconciled these docs at.
The docs-freshness CI reads it: a PR that bumps it is trusted and the AI is skipped
(economy path). See the "Docs freshness" rule in AGENTS.md / CLAUDE.md.

Before a PR: update stale docs, set verified_at_commit to your branch tip.
On arrival: if this is behind the code you touch, reconcile the docs FIRST.

verified_at_commit: 9ffa4007c1369ebb31378234e2096540190964a2
verified_at: 2026-09-24
by: Claude Code
note: Reconciled adapter v3.2.0: per-call event instance_id from Core's envelope, the YGGDRASIL_RUN_TOKEN event bearer versus the disabled-by-default legacy publish dispatcher pair, and the destroy_service_invoice invoice_id input.
