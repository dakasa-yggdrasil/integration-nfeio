# AI docs freshness stamp

Records the commit an AI (or agent-assisted human) last reconciled these docs at.
The docs-freshness CI reads it: a PR that bumps it is trusted and the AI is skipped
(economy path). See the "Docs freshness" rule in AGENTS.md / CLAUDE.md.

Before a PR: update stale docs, set verified_at_commit to your branch tip.
On arrival: if this is behind the code you touch, reconcile the docs FIRST.

verified_at_commit: c30333699c33390534ac29bfd56a07e1412e0d09
verified_at: 2026-09-05
by: Codex
note: Reconciled exact-ID NFe.io webhook adoption, TLS enforcement, secret boundaries, capability manifests, and operator documentation.
