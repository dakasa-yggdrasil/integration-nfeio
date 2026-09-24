# Operations — integration-nfeio

Health, readiness, metrics, the webhook runbook, and common failure modes. Sourced
from `cmd/adapter/main.go`, `providers/nfeio/adapter/spec.go` (metrics),
`providers/nfeio/adapter/webhook_server.go`, and `deploy/service.yaml`.
Back to the [README](../README.md).

---

## Health & readiness

Served by the health server on `HEALTHCHECK_PORT` (default `8080`):

| Endpoint | Behavior |
|---|---|
| `GET /healthz` | Liveness. Always `200 ok`. |
| `GET /readyz` | Readiness. Always `200 ready`. The transport handles reconnect internally, so adapter readiness is effectively "templates loaded and main loop running". |
| `GET /metrics` | Prometheus exposition (see below). |

> Unlike the broker-coupled template default, `/readyz` here does **not** flip to 503
> on transport loss — both probes always return 200. Use the metrics + RPC describe
> path to detect degradation.

## Metrics

Seven Prometheus series registered in `providers/nfeio/adapter/spec.go`, exposed at
`:8080/metrics`:

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `nfeio_rate_limit_remaining` | gauge | — | `X-RateLimit-Remaining` from the last NFe.io response. |
| `nfeio_webhook_received_total` | counter | `status` | Inbound webhooks by normalized status (`issued`/`cancelled`/`processing_failed`). |
| `nfeio_dedup_hits_total` | counter | — | LRU dedup hits (duplicate webhook events). |
| `nfeio_request_duration_seconds` | histogram | `op` | Duration of each NFe.io HTTP call, by capability. |
| `nfeio_request_errors_total` | counter | `op`, `status` | Failed NFe.io requests by HTTP status code. |
| `nfeio_template_load_total` | gauge | `municipio` | `1` per loaded município template at startup. |
| `nfeio_bulk_issue_items_total` | counter | `result` | `bulk_issue` items processed, keyed `success`/`error`. |

An OpenTelemetry span is emitted per NFe.io HTTP call (`go.opentelemetry.io/otel`).

## Ports & Service

| Port | Server | In `deploy/service.yaml`? |
|---|---|---|
| `8080` | health/metrics | yes (`health`) |
| `8081` | RPC (`/rpc/describe`, `/rpc/execute`) | yes (`rpc`) |
| `8082` | legacy normalized-body listener (`/webhook/nfeio`) | **no**; do not route provider ingress here |

> `deploy/service.yaml` must expose both `8080` **and** `8081`. Pre-2.2.3 the live
> Service only declared `8080`, so `yggdrasil-core` forward-drift auto-sync hit
> "connection refused" reaching `/rpc/describe` over Service DNS. The fix is to apply
> the source manifest (which declares both named ports) — not to `kubectl exec`
> around it. The Service runs in the `dakasa` namespace.

## Webhook runbook

> This runbook covers the legacy normalized-body compatibility listener only.
> It does not implement NFe.io's current provider payload contract. Keep it
> unexposed. DaKasa production routes the provider to Payments.

The webhook listener (`:8082`, `/webhook/nfeio`) implements the `nfse_webhook_received`
reactor pipeline. Failure modes and their responses:

| Symptom | HTTP | Cause | Action |
|---|---|---|---|
| `read body` | 400 | Body unreadable | Transient; NFe.io will retry. |
| `invalid signature` | 401 | HMAC mismatch | Confirm `NFEIO_WEBHOOK_SECRET` matches the secret set in NFe.io → Webhooks. The signature is verified over the **raw** request bytes. |
| `decode` | 400 | Body is not the legacy normalized JSON envelope | Do not route current NFe.io provider traffic here. |
| `{"status":"duplicate"}` | 200 | Event already seen (LRU 4096 by `id`, body-hash fallback) | Expected on NFe.io retries; watch `nfeio_dedup_hits_total`. |
| `202 Accepted` (logged, not enqueued) | 202 | Unknown/unmapped event | Event not in the normalize table; check `webhook unknown event` logs. Returning 202 avoids an NFe.io retry storm. |
| `publish failed` | 500 | `publish_message` to core failed | Only possible with the dispatcher enabled (`YGGDRASIL_CORE_BASE_URL` and `YGGDRASIL_WORKFLOW_RUN_TOKEN` both set). Core has no `/api/v1/capabilities/invoke` route, so expect 404; disable the dispatcher by unsetting those env vars. |

The publish dispatcher would route through `yggdrasil-core`
(`POST /api/v1/capabilities/invoke`, `capability: publish_message`) onto the
`rabbitmq-topology` instance and the
`enterprise-payments.nfe.{emitted,rejected,canceled}.q` queues. Core has no such
route, so the dispatcher is disabled unless `YGGDRASIL_CORE_BASE_URL` and
`YGGDRASIL_WORKFLOW_RUN_TOKEN` are both set. When it is disabled the adapter logs
`legacy webhook publish dispatcher disabled` at startup, and the listener logs
`webhook publisher not wired; dropping event` and drops each message. That is the
expected state. The dispatcher never reads `YGGDRASIL_CORE_URL` or
`YGGDRASIL_RUN_TOKEN`.

## Mutation events

With `YGGDRASIL_CORE_URL` set, every successful ensure or destroy through the
reconcilers posts an event to `POST /api/v1/events` with `YGGDRASIL_RUN_TOKEN`
as the bearer. `instance_id` is Core's per-call `integration.instance.name`
(`nfeio-dakasa-production` or `nfeio-dakasa-validation` for DaKasa), and
`idempotency` is Core's `metadata.idempotency` when present. Emission is best
effort, so a refused event only shows up as an adapter WARN:

| Log line | Cause | Action |
|---|---|---|
| `reconcile: emit "nfeio.<resource>.<verb>" failed ... terminal status 401` | Core does not accept the bearer | Check that `YGGDRASIL_RUN_TOKEN` is the adapter's own event publish token. |
| `... terminal status 403` | The publisher has no grant for this event type and instance, or the envelope carried no `integration.instance.name` (empty `instance_id`) | Check the principal grants in Core. An empty instance is the expected fail-closed result; fix the caller, not the adapter. |
| `... terminal status 400` | A required event field was empty or malformed | Read the problem detail in the WARN. |
| `events: noop emitter suppressed mutation event` | `YGGDRASIL_CORE_URL` is unset | Set it to the Core Service URL. |

Proof that events land is a read of Core's `event_log`, not the absence of
WARNs.

## Common failures

| Failure | Likely cause |
|---|---|
| Worker exits on boot with `config load` fatal | `NFEIO_API_KEY` is unset or `NFEIO_WEBHOOK_SECRET` is outside 32 to 64 characters or has surrounding whitespace. |
| Worker exits with `template load` fatal | `TEMPLATES_DIR` points at an unreadable dir and the embedded fallback also failed. |
| `YGGDRASIL_TRANSPORT=amqp` fatal at boot | `BROKER_URL` is empty under AMQP transport. |
| Describe registration rejected by core | Live `Describe()` shape drifted from the stored `integration_type` manifest. Re-check `spec.go` vs `manifest/integration_type.nfeio.yaml`. |
| `destroy_service_invoice` keeps failing 422 | `cancellation_window_closed` is terminal — the NFSe cancellation window is closed; compensate downstream instead of retrying. |
| `destroy_service_invoice: ref (invoice_id) required` | The input had neither `invoice_id` nor `ref`. Since v3.2.0 the documented `invoice_id` is enough. |

## Cross-references

- Env vars and ports: [CONFIGURATION.md](./CONFIGURATION.md).
- Reactor pipeline detail: [CAPABILITIES.md](./CAPABILITIES.md#reactor-nfse_webhook_received).
</content>
