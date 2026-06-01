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
| `8082` | webhook (`/webhook/nfeio`) | **no** — routed via cluster ingress to the dedicated listener |

> `deploy/service.yaml` must expose both `8080` **and** `8081`. Pre-2.2.3 the live
> Service only declared `8080`, so `yggdrasil-core` forward-drift auto-sync hit
> "connection refused" reaching `/rpc/describe` over Service DNS. The fix is to apply
> the source manifest (which declares both named ports) — not to `kubectl exec`
> around it. The Service runs in the `dakasa` namespace.

## Webhook runbook

The webhook listener (`:8082`, `/webhook/nfeio`) implements the `nfse_webhook_received`
reactor pipeline. Failure modes and their responses:

| Symptom | HTTP | Cause | Action |
|---|---|---|---|
| `read body` | 400 | Body unreadable | Transient; NFe.io will retry. |
| `invalid signature` | 401 | HMAC mismatch | Confirm `NFEIO_WEBHOOK_SECRET` matches the secret set in NFe.io → Webhooks. The signature is verified over the **raw** request bytes. |
| `decode` | 400 | Body is not the expected JSON envelope | Inspect the payload; NFe.io schema change. |
| `{"status":"duplicate"}` | 200 | Event already seen (LRU 4096 by `id`, body-hash fallback) | Expected on NFe.io retries; watch `nfeio_dedup_hits_total`. |
| `202 Accepted` (logged, not enqueued) | 202 | Unknown/unmapped event | Event not in the normalize table; check `webhook unknown event` logs. Returning 202 avoids an NFe.io retry storm. |
| `publish failed` | 500 | `publish_message` to core failed | NFe.io retries. Check `YGGDRASIL_CORE_URL`, `YGGDRASIL_RUN_TOKEN`, and that the `rabbitmq-topology` instance (`RABBITMQ_TOPOLOGY_INSTANCE`) is healthy. |

Publishing routes through `yggdrasil-core` (`POST /api/v1/capabilities/invoke`,
`capability: publish_message`) onto the `rabbitmq-topology` instance and lands in the
`enterprise-payments.nfe.{emitted,rejected,canceled}.q` queues. If the publisher is
not wired (no dispatcher set), the server logs `webhook publisher not wired; dropping
event` and drops the message — verify `YGGDRASIL_CORE_URL` is set at startup.

## Common failures

| Failure | Likely cause |
|---|---|
| Worker exits on boot with `config load` fatal | `NFEIO_API_KEY` or `NFEIO_WEBHOOK_SECRET` unset — both are mandatory. |
| Worker exits with `template load` fatal | `TEMPLATES_DIR` points at an unreadable dir and the embedded fallback also failed. |
| `YGGDRASIL_TRANSPORT=amqp` fatal at boot | `BROKER_URL` is empty under AMQP transport. |
| Describe registration rejected by core | Live `Describe()` shape drifted from the stored `integration_type` manifest. Re-check `spec.go` vs `manifest/integration_type.nfeio.yaml`. |
| `destroy_service_invoice` keeps failing 422 | `cancellation_window_closed` is terminal — the NFSe cancellation window is closed; compensate downstream instead of retrying. |

## Cross-references

- Env vars and ports: [CONFIGURATION.md](./CONFIGURATION.md).
- Reactor pipeline detail: [CAPABILITIES.md](./CAPABILITIES.md#reactor-nfse_webhook_received).
</content>
