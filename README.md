# integration-nfeio

Yggdrasil integration adapter for [NFe.io](https://nfe.io) — Brazilian municipal service invoices (NFSe).

## Capabilities

- `issue_nfse`, `get_nfse_status`, `cancel_nfse`, `retrieve_pdf`, `retrieve_xml`
- `register_company`, `list_municipalities`, `manage_template`
- `bulk_issue` (up to 50 NFSe per call)
- `calculate_iss` (pure-function sub-capability)
- Reactor `nfse_webhook_received` (NFe.io status webhook → RabbitMQ)

## Quickstart

```bash
yggdrasil install dakasa-yggdrasil/integration-nfeio
```

## License

Apache 2.0.
