# NFe.io exact-ID HMAC migration handoff

Date: 2026-09-05

Repository: `github.com/dakasa-yggdrasil/integration-nfeio`

Branch: `codex/nfeio-hmac-migration-20260905`

Base: `98fbb7f`

## Decision

The existing production webhook must be upgraded in place instead of recreated.
The adapter now supports that upgrade through the existing
`ensure_webhook_subscription` resource capability. Ordinary reconciliation
still changes only `insecureSsl=true` to `false`. The new security path requires
an exact provider ID, three explicit migration fields, and the HMAC already
projected into the runtime credential.

No provider, AWS, Kubernetes, database, workflow catalog, secret, deployment,
commit, push, tag, or release was mutated while producing this handoff.

## Read-only production evidence

The operator-supplied file at `/Users/dakasa/Desktop/nfeio-creds.txt` contains
an NFe.io company ID and API key. Values were never printed or copied into the
repository.

Read-only provider calls proved:

- the API key is accepted with HTTP 200;
- the exact company is accessible with HTTP 200;
- exactly one webhook exists;
- the webhook is Active, has the canonical Payments callback URI, and has seven
  filters;
- the provider object still has `insecureSsl=true`;
- the provider object carries a legacy `Authorization` callback header;
- the provider GET response omits the HMAC field, so the old HMAC cannot be
  recovered;
- the exact provider ID was deliberately kept out of source and logs.

The NFe.io documentation requires a 32 to 64 character webhook secret and uses
it to generate HMAC-SHA1 over the exact callback body in `X-Hub-Signature`:

- https://nfe.io/docs/desenvolvedores/rest-api/nota-fiscal-de-servico-v1/criar-um-webhook/
- https://nfe.io/docs/documentacao/webhooks/duvidas-frequentes/

## Implementation contract

Adapter version `3.1.0` extends the exact-ID ensure input with an optional
atomic migration:

```json
{
  "id": "<exact-provider-id>",
  "insecure_ssl": false,
  "set_hmac_from_runtime": true,
  "remove_legacy_authorization": true,
  "confirm_security_migration_id": "<same-exact-provider-id>"
}
```

The adapter rejects partial flags, a mismatched confirmation, an invalid exact
ID, and a runtime HMAC outside the provider length contract before any provider
call. The HMAC is read only from the adapter runtime credential. It never enters
the workflow input, desired resource, output, mutation event, error, or log.

The PUT begins with the complete exact-ID GET object, then changes only:

1. `insecureSsl` to `false`;
2. `secret` to the runtime HMAC;
3. removal of every case variant of the legacy `Authorization` header.

All other documented and unknown provider fields remain intact. A confirmation
GET must prove secure TLS and absence of the legacy header. NFe.io does not
return the HMAC after write, so a provider-signed callback received by Payments
is the final proof that the provider stored the same value.

## Validation

The following checks passed:

```text
GOWORK=off go test ./... -count=1
GOWORK=off go vet ./...
GOWORK=off go run ./cmd/lint-action-catalog
GOWORK=off go run ./cmd/validate-templates manifest/templates
git diff --check
```

Focused tests cover the exact GET, PUT, GET sequence, preservation of opaque
provider fields, runtime-only HMAC use, case-insensitive removal of legacy
Authorization, post-PUT confirmation failure, partial input rejection, invalid
runtime credential rejection, and secret-free outputs and errors.

## Production order

1. Materialize one 32 to 64 character random HMAC together with the supplied
   API key and company ID in the canonical us-east-1 secret plane without using
   command arguments or workflow history.
2. Apply Payments migration `00014` and only then roll the new Payments binary.
3. Roll integration-nfeio `3.1.0` with the same canonical HMAC projected as its
   runtime credential.
4. Observe the exact existing webhook and Payments receiver readiness.
5. Dispatch the explicit security migration once with the out-of-band exact ID.
6. Require the confirmation GET, a provider-signed callback accepted by
   Payments, no retry storm, and no fiscal side effect from the validation ping.

Do not create or delete another provider webhook, and do not enable the legacy
Payments Authorization fallback in production.
