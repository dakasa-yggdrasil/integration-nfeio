# Usage — integration-nfeio

End-to-end journey for adopters: install → configure → first workflow → verify.
Back to the [README](../README.md) · upstream control plane:
[`yggdrasil-core`](https://github.com/dakasa-yggdrasil/yggdrasil-core).

This adapter wraps the [NFe.io](https://nfe.io) `/v2` REST API to manage Brazilian
municipal service invoices (NFSe) declaratively. `yggdrasil-core` calls its
`describe` / `execute` endpoints; you drive it from workflow YAML.

---

## 1. Prerequisites

- An NFe.io account with an **API key** (NFe.io → Settings → API) and a
  **webhook HMAC secret** (NFe.io → Webhooks).
- A running `yggdrasil-core` to register the integration type + instance against.
- The adapter image (`ghcr.io/dakasa-yggdrasil/integration-nfeio`) or a local build.

## 2. Run the adapter

There is **no** `yggdrasil-quickstart.yaml`, `docker-compose.yml`, or `Taskfile.yml`
in this repo — run the worker directly. HTTP-JSON is the default transport.

```bash
docker build -t integration-nfeio:dev .

docker run --rm \
  -e NFEIO_API_KEY=sk_live_xxx \
  -e NFEIO_WEBHOOK_SECRET=replace_with_32_char_secret_here \
  -p 8080:8080 -p 8081:8081 -p 8082:8082 \
  integration-nfeio:dev
```

Ports:

| Port | Server | Paths |
|---|---|---|
| `8080` | health | `/healthz`, `/readyz`, `/metrics` |
| `8081` | RPC (HTTP-JSON) | `/rpc/describe`, `/rpc/execute` |
| `8082` | webhook | `/webhook/nfeio` |

The worker is fatal-on-start if `NFEIO_API_KEY` is unset or if
`NFEIO_WEBHOOK_SECRET` is not 32 to 64 characters without surrounding whitespace.

## 3. Register with yggdrasil-core

Apply the manifests under `manifest/` to register the integration type, its
capabilities, and an instance. The shipped instance (`manifest/instance.nfeio.yaml`)
binds credentials from a secret store and points at the in-cluster Service DNS:

```yaml
# manifest/instance.nfeio.yaml (excerpt)
apiVersion: yggdrasil.io/v1alpha1
kind: integration_instance
metadata:
  name: nfeio-dakasa
  namespace: dakasa
spec:
  integration_type: nfeio
  endpoint: http://integration-nfeio.yggdrasil-adapters.svc.cluster.local:8081
  credentials:
    NFEIO_API_KEY:        { source: aws_secrets_manager, path: dakasa/prod/nfeio-api-key }
    NFEIO_WEBHOOK_SECRET: { source: aws_secrets_manager, path: dakasa/prod/nfeio-webhook-secret }
  config:
    NFEIO_BASE_URL: "https://api.nfe.io"
    NFEIO_COMPANY_ID: ""                       # set per binding when known
    RABBITMQ_TOPOLOGY_INSTANCE: rabbitmq-topology-default
```

> The `credentials.source`/`path` shown above is how the **shipped** instance binds
> secrets — Yggdrasil itself is provider-agnostic (the Lego principle); the adapter
> only ever reads `NFEIO_API_KEY` / `NFEIO_WEBHOOK_SECRET` from its environment.

`yggdrasil-core` calls `GET /rpc/describe` to confirm the live adapter shape matches
the stored `integration_type` manifest before activating the instance.

## 4. First workflow — issue an NFSe

`ensure_service_invoice` is idempotent on `external_id`: a re-run that hits a 409
duplicate at NFe.io returns the existing invoice (success), so retries are safe.

```yaml
- id: issue-invoice
  uses: integration.nfeio.ensure_service_invoice
  with:
    municipio_code: "3550308"            # São Paulo (bundled template)
    external_id: "order-9c2f"
    borrower_name: "Acme Servicos LTDA"
    borrower_federal_tax_number: 12345678000190
    borrower_address:
      street: "Av Paulista"
      number: "1000"
      city: "São Paulo"
    service_amount: 1500.00
    description: "Consultoria de software"
```

The município code selects the bundled template that supplies the ISS rate and
service codes. Five templates ship out of the box:

| Code | Municipality | State |
|---|---|---|
| `3550308` | São Paulo | SP |
| `3304557` | Rio de Janeiro | RJ |
| `4106902` | Curitiba | PR |
| `4205407` | Florianópolis | SC |
| `3106200` | Belo Horizonte | MG |

## 5. Verify the run

Read the invoice back (single-resource lookup via `{invoice_id}` filter):

```yaml
- id: check-invoice
  uses: integration.nfeio.observe_service_invoices
  with:
    invoice_id: "{{ steps.issue-invoice.outputs.id }}"
```

`observe_service_invoices` output includes `status` / `flow_status`. Route real
NFe.io callbacks to an application receiver that implements the current signed
`payload`, `X-Hook-Id`, and `X-Hook-Event` contract. The adapter-local listener
accepts only a legacy normalized body and must remain unexposed from NFe.io.

## 6. Other common flows

- **Bulk issue** — `bulk_issue` accepts up to **50** items, runs them with a
  concurrency cap of 5, and returns per-item results (`succeeded_count` /
  `failed_count`); partial failures are not an error. > 50 items is a terminal
  `input_too_large`.
- **Cancel** — `destroy_service_invoice` with `{invoice_id}`. A 422
  `cancellation_window_closed` is terminal (compensate, don't retry); 404 is treated
  as already-absent success.
- **PDF / XML** — `retrieve_pdf` / `retrieve_xml` return a signed download URL.
- **Pre-flight tax** — `calculate_iss` computes the ISS amount from a município
  template with no network call.

See [CAPABILITIES.md](./CAPABILITIES.md) for every input/output schema.
</content>
