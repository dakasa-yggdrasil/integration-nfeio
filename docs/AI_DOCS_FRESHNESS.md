# AI docs freshness stamp

Records the commit an AI (or agent-assisted human) last reconciled these docs at.
The docs-freshness CI reads it: a PR that bumps it is trusted and the AI is skipped
(economy path). See the "Docs freshness" rule in AGENTS.md / CLAUDE.md.

Before a PR: update stale docs, set verified_at_commit to your branch tip.
On arrival: if this is behind the code you touch, reconcile the docs FIRST.

verified_at_commit: 5d82503fa53b5cb6dda3a72cb7959e2a96953406
verified_at: 2026-09-06
by: Codex
note: Reconciled adapter v3.1.2 canonical credential schema keys with the unchanged NFEIO runtime environment bindings.
