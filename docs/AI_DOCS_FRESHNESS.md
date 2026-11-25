# AI docs freshness stamp

Records the commit an AI (or agent-assisted human) last reconciled these docs at.
The docs-freshness CI reads it: a PR that bumps it is trusted and the AI is skipped
(economy path). See the "Docs freshness" rule in AGENTS.md / CLAUDE.md.

Before a PR: update stale docs, set verified_at_commit to your branch tip.
On arrival: if this is behind the code you touch, reconcile the docs FIRST.

verified_at_commit: 13e8f7dced8a3c43b394df794b736fb835f504b7
verified_diff_sha256: 75e86a9405f90c70b24167cda77c4a5f9ac591a1877e793b8bc38c7123b7477c
reconciler_schema: 1
verified_at: 2026-11-24
by: Codex
note: Reconciled signed SDK v0.10.0 same-instance original drain, auxiliary listener admission cancellation, joined original webhook shutdown, and quickstart grace45. Provider operations, HMAC/dedup/dispatcher/response contracts remain unchanged. CI-only concurrency checks; no local tests/builds or runtime adoption.
