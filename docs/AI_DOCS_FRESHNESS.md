# AI docs freshness stamp

Records the commit an AI (or agent-assisted human) last reconciled these docs at.
The docs-freshness CI reads it: a PR that bumps it is trusted and the AI is skipped
(economy path). See the "Docs freshness" rule in AGENTS.md / CLAUDE.md.

Before a PR: update stale docs, set verified_at_commit to your branch tip.
On arrival: if this is behind the code you touch, reconcile the docs FIRST.

verified_at_commit: d895524d9533710ae22b6fc7c6a1ee024fd0bc51
verified_at: 2026-09-24
by: Claude Code
note: Reconciled adapter v3.2.0 after review: per-call event instance_id with only nfeio-dakasa-production granted (one credential set), idempotency only from direct callers, the event bearer versus the disabled-by-default publish dispatcher pair as a dakasa-system prerequisite, destroy_service_invoice success only on Cancelled (cancellation_pending otherwise, 404 is an error), caller id precedence, and escaped NFe.io path segments.
