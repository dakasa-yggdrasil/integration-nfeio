# NFe.io exact-ID webhook reconciliation handoff

Date: 2026-09-05

## Decision

**BLOCK production rollout. GO for code review and integration.**

The local implementation is complete and all local gates pass. Production remains
blocked because this task explicitly prohibited live mutation, the exact production
webhook ID was not supplied or discovered, the real account response shape was not
observed, and v3 removes compatibility aliases. Do not deploy or invoke the update
until the rollout gates below are satisfied.

## Scope delivered

- `ensure_webhook_subscription` now reconciles one existing NFe.io webhook by exact
  provider ID.
- The only allowed desired state is `insecure_ssl=false`.
- A no-op performs one exact-ID GET and no mutation.
- Drift performs exact-ID GET, full-object PUT with only provider field
  `insecureSsl` changed to `false`, then an exact-ID confirmation GET.
- No ensure or observe path POSTs, lists, matches by URI, discovers, duplicates, or
  deletes webhooks.
- Provider `secret`, URI, headers, properties, raw payload, and unknown fields are
  preserved inside the PUT request but never copied into resource, adoption,
  mutation-event, error, or log output.
- `destroy_webhook_subscription` remains available only as an explicit operator
  action. It requires `confirm_id` to exactly match `id`, is removed from webhook
  `default_actions`, and the generic SDK destroy method is non-mutating.
- The breaking input/behavior change is represented as adapter v3.0.0. The v2
  legacy aliases were removed at the major boundary as required by the integration
  contract.

## Exact wire contract

Ensure input:

```json
{"id":"<exact-provider-id>","insecure_ssl":false}
```

Safe ensure output:

```json
{"id":"<exact-provider-id>","insecure_ssl":false,"adopted":true,"updated":true}
```

Observe input:

```json
{"id":"<exact-provider-id>"}
```

Explicit destroy input:

```json
{"id":"<exact-provider-id>","confirm_id":"<same-exact-provider-id>"}
```

Yggdrasil reserved keys prefixed with `__` remain accepted and ignored by the
provider-field validator. Other unknown fields, including the old URL/events create
shape, fail before any provider request.

## Provider contract evidence

The implementation follows the official NFe.io v2 exact-resource endpoints:

- Exact lookup: `GET /v2/webhooks/{webhookId}`, documented in
  https://nfe.io/docs/desenvolvedores/rest-api/nota-fiscal-de-servico-v1/registration-lookup-action/
- Exact update: `PUT /v2/webhooks/{webhookId}` with the `webHook` envelope,
  documented in
  https://nfe.io/docs/desenvolvedores/rest-api/nota-fiscal-de-servico-v1/alterar-um-webhook-existente/
- Provider webhook fields are treated as opaque JSON except for validated `id` and
  boolean `insecureSsl`. This is intentional because the object may contain
  `secret`, `headers`, `properties`, `subscription`, version/timestamps, and future
  fields.

No NFe.io API call was made during this task.

## Intake and repository state

- Repository: `/Users/dakasa/projects/dakasa/yggdrasil/integration-nfeio`
- Clean intake base: `299dbe0`
- Branch: local `main`
- At intake and handoff, local `main` is two commits behind `origin/main`:
  - `a75cf9f` (`emit-deploy-event` gate)
  - `d439aaf` (documentation freshness convention)
- No rebase, pull, commit, push, PR, tag, release, deploy, or live provider mutation
  was performed.
- Applicable workspace/repo `AGENTS.md` and `CLAUDE.md` were read in full.
- No adapter-local `INTEGRATION_CONTRACT.md` exists. The complete canonical copy at
  `../integration-template/INTEGRATION_CONTRACT.md` was read; it differs from the
  Didit copy only in template wording.
- `../surface-template/SURFACE_CONTRACT.md` was read in full. No surface code is in
  scope.
- There were no pre-existing `docs/superpowers/*-handoff.md` files in this repo.
- `AGENTS.md`, `CLAUDE.md`, and `GEMINI.md` were changed intentionally to keep their
  version/capability maps aligned with authoritative `Describe()`. They were clean
  at intake and are not generated or unrelated changes.

## Files changed

Runtime and contract:

- `providers/nfeio/adapter/webhook_subscription.go` (new)
- `providers/nfeio/adapter/adapter.go`
- `providers/nfeio/adapter/reconcilers.go`
- `providers/nfeio/adapter/spec.go`
- `cmd/adapter/main.go`

Tests:

- `providers/nfeio/adapter/webhook_subscription_test.go`
- `providers/nfeio/adapter/reconcilers_test.go`
- `providers/nfeio/adapter/spec_test.go`

Manifests and installer:

- `manifest/capability.ensure_webhook_subscription.yaml`
- `manifest/capability.observe_webhook_subscriptions.yaml`
- `manifest/capability.destroy_webhook_subscription.yaml`
- `manifest/integration_type.nfeio.yaml`
- `yggdrasil-quickstart.yaml`

Documentation:

- `README.md`
- `CHANGELOG.md`
- `docs/CAPABILITIES.md`
- `docs/DEVELOPMENT.md`
- `AGENTS.md`
- `CLAUDE.md`
- `GEMINI.md`
- this handoff

## Adversarial coverage

Tests lock the following invariants:

- secure no-op sends exactly one GET and no PUT/POST/DELETE;
- drift sends exactly GET, PUT, GET on the same validated ID;
- the PUT object deep-equals the GET object except for `insecureSsl=false`, including
  secret, authorization header, properties, subscription, timestamps, and an
  unknown future field;
- old URL/events creation shape fails with zero provider calls;
- missing ID, missing `insecure_ssl`, `insecure_ssl=true`, and path-injection IDs
  fail with zero provider calls;
- provider identity mismatch fails after GET and before PUT;
- list/cursor/empty observe inputs fail with zero provider calls;
- output, adoption response, emitted mutation event, errors, and captured logs are
  checked against secret canaries and secret-bearing field names;
- destroy without exact confirmation fails with zero provider calls;
- confirmed exact-ID 404 destroy is idempotent success;
- old webhook create/list/delete aliases fail with zero provider calls;
- `destroy_webhook_subscription` is absent from webhook default actions.

## Validation evidence

All commands ran from the adapter repository with `GOWORK=off` where applicable,
because the workspace-level `/Users/dakasa/projects/dakasa/go.work` does not include
this standalone module.

```text
GOWORK=off go test -count=1 ./...
PASS: all packages; adapter package 5.326s

GOWORK=off go test -race -count=1 ./providers/nfeio/adapter
PASS: adapter package 6.610s

GOWORK=off go test -count=1 -run 'WebhookSubscription|LegacyWebhookAliases' -v ./providers/nfeio/adapter
PASS: all exact-ID, preservation, secret-boundary, alias, and destroy-guard tests

GOWORK=off go vet ./...
PASS

GOWORK=off go run ./cmd/lint-action-catalog
lint-action-catalog: OK

GOWORK=off go run ./cmd/validate-templates manifest/templates
validate-templates: 5 templates OK

ruby YAML parse of the three webhook manifests, integration_type manifest, and quickstart
yaml: OK (5 files)

git diff --check
PASS

docker build --build-arg VERSION=3.0.0-local -t integration-nfeio:codex-webhook-guard .
PASS
```

Local container smoke used only dummy credentials and made no provider call:

- `POST /rpc/describe`: success; descriptor reports adapter v3.0.0.
- `/readyz`: 200 `ready`.
- `/metrics`: 200.
- The first immediate `/healthz` probe returned a transient local 403 while Docker
  port forwarding was settling; the subsequent probe returned 200 `ok`.
- Local smoke container was stopped and removed. The local image tag remains.

## Production rollout gates

1. Integrate the two missing `origin/main` commits non-destructively, then rerun all
   gates.
2. Audit stored workflows, executions, and callers for every removed v2 alias before
   registering v3. Do not infer absence from repository search alone.
3. Obtain the exact production webhook ID from the NFe.io account without copying
   secret-bearing response fields into tickets, logs, chat, or repository files.
4. Perform a read-only exact-ID observation against the real provider and confirm
   the official `webHook` shape. Redact the response; record only ID, HTTP status,
   and `insecureSsl` state.
5. Quiesce edits to that webhook during the full-object PUT window. NFe.io's
   documented update is PUT rather than field PATCH, so a concurrent provider-side
   edit between GET and PUT is the remaining overwrite risk.
6. Verify the public callback ingress certificate and routing after the TLS cut.
7. Deploy/register v3, run exact-ID observe first, then invoke ensure with
   `insecure_ssl=false`. Confirm delivery through the inbound webhook and downstream
   `enterprise-payments.nfe.*` path.
8. Keep destroy out of rollout automation. Use it only for a separately approved
   operator action with matching `id` and `confirm_id`.

## Residual risks

- No real NFe.io response or authenticated read was exercised here.
- Full-object PUT cannot eliminate a concurrent-edit race without provider support
  for conditional updates. Use an operator maintenance window unless an ETag or
  equivalent precondition is proven from the real API.
- v3 alias removal is contract-correct but requires live caller inventory before
  rollout.
- The local Docker health probe had one transient 403 before succeeding; this did
  not reproduce on retry, but production health behavior should still be checked as
  part of the rollout.
