# Changelog

## v1.0.0 — 2026-05-26

Initial release.

- 10 capabilities (`issue_nfse`, `get_nfse_status`, `cancel_nfse`, `retrieve_pdf`,
  `retrieve_xml`, `register_company`, `list_municipalities`, `manage_template`,
  `bulk_issue`, `calculate_iss`)
- Reactor `nfse_webhook_received` (HMAC verify + LRU dedup + RabbitMQ publish
  via `publish_message` on the rabbitmq-topology instance)
- 5 município templates (São Paulo, Rio de Janeiro, Curitiba, Florianópolis,
  Belo Horizonte)
- SDK pin: yggdrasil-sdk-go v0.4.0 (uses `sdk/webhookhttp` for HMAC-SHA256
  signature verification)
- 7 Prometheus metrics on `/metrics` + OTel span per HTTP call
- Multi-arch (amd64 + arm64), distroless/base-debian12:nonroot
