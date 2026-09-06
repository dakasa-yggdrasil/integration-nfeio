package adapter

import (
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/dakasa-yggdrasil/integration-nfeio/family/contract"
)

// Prometheus metrics shared across the adapter. The set mirrors spec §10
// observability requirements: request duration histogram + error counter
// (keyed by capability + status), webhook receive + dedup counters,
// template-load gauge, bulk_issue item counter, rate-limit gauge.
var (
	metricRateLimitRemaining = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nfeio_rate_limit_remaining",
		Help: "X-RateLimit-Remaining from last NFe.io response.",
	})

	metricWebhookReceived = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nfeio_webhook_received_total",
		Help: "Inbound webhooks by normalized status.",
	}, []string{"status"})

	metricDedupHits = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "nfeio_dedup_hits_total",
		Help: "LRU dedup cache hits (duplicate webhook events).",
	})

	metricRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "nfeio_request_duration_seconds",
		Help: "Duration of each NFe.io HTTP call, by capability.",
	}, []string{"op"})

	metricRequestErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nfeio_request_errors_total",
		Help: "Failed NFe.io requests by HTTP status code.",
	}, []string{"op", "status"})

	metricTemplateLoad = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nfeio_template_load_total",
		Help: "1 per loaded template at startup, keyed by municipio code.",
	}, []string{"municipio"})

	metricBulkIssueItems = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nfeio_bulk_issue_items_total",
		Help: "Items processed by bulk_issue, keyed by result (success|error).",
	}, []string{"result"})
)

func init() {
	prometheus.MustRegister(
		metricRateLimitRemaining,
		metricWebhookReceived,
		metricDedupHits,
		metricRequestDuration,
		metricRequestErrors,
		metricTemplateLoad,
		metricBulkIssueItems,
	)
}

// MetricTemplateLoad exposes the template-load gauge so main.go can flip
// 1 per loaded município at startup without importing the prometheus
// package in cmd/adapter.
func MetricTemplateLoad() *prometheus.GaugeVec { return metricTemplateLoad }

const (
	// Provider is the family id. Matches the integration_type spec.provider
	// field that yggdrasil-core sends in the describe handshake.
	Provider = "nfeio"
	// IntegrationType is the per-provider id. SDK uses it to derive the AMQP
	// queue prefix yggdrasil.adapter.nfeio.{describe,execute}.
	IntegrationType = "nfeio"

	// v2.0.0 canonical capability names (per docs/superpowers/specs/2026-05-27-yggdrasil-integration-capability-convention.md §7).
	OpEnsureServiceInvoice   = "ensure_service_invoice"   // collapses pre-v2.0.0 issue_nfse
	OpObserveServiceInvoices = "observe_service_invoices" // merges pre-v2.0.0 get_nfse_status (filter by id)
	OpDestroyServiceInvoice  = "destroy_service_invoice"  // collapses pre-v2.0.0 cancel_nfse
	OpRetrievePDF            = "retrieve_pdf"             // kept (allowlist helper — file URL retrieval)
	OpRetrieveXML            = "retrieve_xml"             // kept (allowlist helper — file URL retrieval)
	OpEnsureCompany          = "ensure_company"           // collapses pre-v2.0.0 register_company
	OpObserveCompanies       = "observe_companies"        // NEW: GET /v2/companies (by federal_tax_number filter or list)
	OpObserveMunicipalities  = "observe_municipalities"   // renames pre-v2.0.0 list_municipalities
	OpManageTemplate         = "manage_template"          // kept (control-plane action, not external resource)
	OpBulkIssue              = "bulk_issue"               // kept (bulk action — documented special case)
	OpCalculateISS           = "calculate_iss"            // kept (allowlist — pure function helper)

	// webhook_subscription resource lifecycle.
	OpEnsureWebhookSubscription   = "ensure_webhook_subscription"
	OpObserveWebhookSubscriptions = "observe_webhook_subscriptions"
	OpDestroyWebhookSubscription  = "destroy_webhook_subscription"

	ReactorWebhookReceived = "nfse_webhook_received"

	QueueDescribe = "yggdrasil.adapter.nfeio.describe"
	QueueExecute  = "yggdrasil.adapter.nfeio.execute"

	ResourceServiceInvoice       = "service_invoice"
	ResourceCompany              = "company"
	ResourceMunicipality         = "municipality"
	ResourceMunicipalityTemplate = "municipality_template"
	ResourceWebhookSubscription  = "webhook_subscription"
)

// AdapterVersion is the single source for the version advertised by the
// running adapter. Release builds override this variable with the same value
// read from this source file so the image identity and Describe stay aligned.
var AdapterVersion = "3.1.2"

// SupportedExecuteOperations lists the operations callable via the execute
// RPC path. The reactor (nfse_webhook_received) is intentionally excluded —
// it is triggered by the webhook HTTP server, not RPC.
//
// v2.0.0: 14 entries (3 canonical triples for service_invoice / company /
// webhook_subscription, plus observe_companies + observe_municipalities, plus
// kept-in-place helpers retrieve_pdf, retrieve_xml, manage_template,
// bulk_issue, calculate_iss).
var SupportedExecuteOperations = []string{
	OpEnsureServiceInvoice, OpObserveServiceInvoices, OpDestroyServiceInvoice,
	OpRetrievePDF, OpRetrieveXML,
	OpEnsureCompany, OpObserveCompanies,
	OpObserveMunicipalities,
	OpManageTemplate, OpBulkIssue, OpCalculateISS,
	OpEnsureWebhookSubscription, OpObserveWebhookSubscriptions, OpDestroyWebhookSubscription,
}

// Describe returns the contract the adapter implements. Transport is selected
// at startup via YGGDRASIL_TRANSPORT (default http_json). The core uses this
// to verify the adapter's live shape matches the stored integration_type
// manifest.
func Describe() contract.AdapterDescribeResponse {
	transport := strings.ToLower(strings.TrimSpace(os.Getenv("YGGDRASIL_TRANSPORT")))
	if transport == "" {
		transport = "http_json"
	}
	adapterSpec := contract.IntegrationAdapterSpec{
		Version:        AdapterVersion,
		TimeoutSeconds: 60,
	}
	switch transport {
	case "amqp", "rabbitmq":
		adapterSpec.Transport = "rabbitmq"
		adapterSpec.Queues = contract.IntegrationAdapterQueue{
			Describe: QueueDescribe,
			Execute:  QueueExecute,
		}
	default:
		adapterSpec.Transport = "http_json"
		adapterSpec.Endpoints = contract.IntegrationAdapterRoute{
			Describe: "/rpc/describe",
			Execute:  "/rpc/execute",
		}
	}
	return contract.AdapterDescribeResponse{
		Provider:     Provider,
		Adapter:      adapterSpec,
		Capabilities: []string{"describe", "execute"},
		CredentialSchema: contract.IntegrationSchemaSpec{
			Mode:     "inline",
			Required: []string{"nfeio_api_key", "nfeio_webhook_secret"},
			Properties: map[string]contract.IntegrationSchemaProperty{
				"nfeio_api_key": {
					Type:        "string",
					Label:       "NFe.io API key",
					LabelLocale: map[string]string{"pt-BR": "Chave da API NFe.io", "en-US": "NFe.io API key"},
					Placeholder: "Cole a API key gerada no painel NFe.io",
					PlaceholderLocale: map[string]string{
						"pt-BR": "Cole a API key gerada no painel NFe.io",
						"en-US": "Paste the API key generated in the NFe.io dashboard",
					},
					Description: "API key gerada em NFe.io → Configurações → API. Usada para todas as chamadas REST autenticadas.",
					DescriptionLocale: map[string]string{
						"pt-BR": "Encontre em NFe.io → Configurações → API. Necessária para autenticar chamadas REST.",
						"en-US": "Find in NFe.io → Settings → API. Used to authenticate REST calls.",
					},
					Group:       "NFe.io credentials",
					GroupLocale: map[string]string{"pt-BR": "Credenciais NFe.io", "en-US": "NFe.io credentials"},
					Order:       1,
					Sensitive:   true,
					Secret:      true,
				},
				"nfeio_webhook_secret": {
					Type:        "string",
					Label:       "Webhook HMAC secret",
					LabelLocale: map[string]string{"pt-BR": "Segredo HMAC do webhook", "en-US": "Webhook HMAC secret"},
					Placeholder: "Segredo configurado no webhook NFe.io",
					PlaceholderLocale: map[string]string{
						"pt-BR": "Segredo configurado no webhook NFe.io",
						"en-US": "Secret configured on the NFe.io webhook",
					},
					Description: "Segredo HMAC usado para validar webhooks recebidos da NFe.io. Configurado em NFe.io → Webhooks.",
					DescriptionLocale: map[string]string{
						"pt-BR": "Segredo HMAC usado para validar webhooks recebidos. Configurado em NFe.io → Webhooks.",
						"en-US": "HMAC secret used to verify inbound webhooks. Set in NFe.io → Webhooks.",
					},
					Group:       "NFe.io credentials",
					GroupLocale: map[string]string{"pt-BR": "Credenciais NFe.io", "en-US": "NFe.io credentials"},
					Order:       2,
					Sensitive:   true,
					Secret:      true,
				},
			},
		},
		InstanceSchema: contract.IntegrationSchemaSpec{
			Mode: "inline",
			Properties: map[string]contract.IntegrationSchemaProperty{
				"environment": {
					Type:        "string",
					Label:       "NFe.io environment",
					LabelLocale: map[string]string{"pt-BR": "Ambiente NFe.io", "en-US": "NFe.io environment"},
					Description: "Ambiente alvo da API NFe.io. Use sandbox para homologação.",
					DescriptionLocale: map[string]string{
						"pt-BR": "Ambiente alvo da API. Use sandbox para homologação.",
						"en-US": "Target environment. Use sandbox for staging.",
					},
					Group:       "Runtime configuration",
					GroupLocale: map[string]string{"pt-BR": "Configuração de runtime", "en-US": "Runtime configuration"},
					Order:       1,
					Enum:        []any{"production", "sandbox"},
					Default:     "production",
				},
			},
		},
		ResourceTypes: []contract.IntegrationResourceType{
			{
				Name:             ResourceServiceInvoice,
				CanonicalPrefix:  "thirdparty.nfeio.service_invoice",
				IdentityTemplate: "service_invoice.{external_id}",
				Discoverable:     false,
				DefaultActions: []string{
					OpEnsureServiceInvoice, OpObserveServiceInvoices, OpDestroyServiceInvoice,
					OpRetrievePDF, OpRetrieveXML, OpBulkIssue,
				},
			},
			{
				Name:             ResourceCompany,
				CanonicalPrefix:  "thirdparty.nfeio.company",
				IdentityTemplate: "company.{federal_tax_number}",
				Discoverable:     false,
				DefaultActions:   []string{OpEnsureCompany, OpObserveCompanies},
			},
			{
				Name:             ResourceMunicipality,
				CanonicalPrefix:  "thirdparty.nfeio.municipality",
				IdentityTemplate: "municipality.{code}",
				Discoverable:     false,
				DefaultActions:   []string{OpObserveMunicipalities},
			},
			{
				Name:             ResourceMunicipalityTemplate,
				CanonicalPrefix:  "thirdparty.nfeio.municipality_template",
				IdentityTemplate: "municipality_template.{code}",
				Discoverable:     false,
				DefaultActions:   []string{OpManageTemplate, OpCalculateISS},
			},
			{
				Name:             ResourceWebhookSubscription,
				CanonicalPrefix:  "thirdparty.nfeio.webhook_subscription",
				IdentityTemplate: "webhook_subscription.{id}",
				Discoverable:     false,
				DefaultActions: []string{
					OpEnsureWebhookSubscription, OpObserveWebhookSubscriptions,
				},
			},
		},
		ActionCatalog: actionCatalog(),
		Discovery: contract.IntegrationDiscoverySpec{
			Mode:             "push",
			SupportsWebhooks: true,
		},
		Normalization: contract.IntegrationNormalizationSpec{
			ExternalIDPath:         "id",
			NamePath:               "external_id",
			FallbackResourcePrefix: "thirdparty.nfeio.custom",
		},
		Execution: contract.IntegrationExecutionSpec{
			IdempotentActions: append([]string{}, SupportedExecuteOperations...),
		},
		Extensions: contract.IntegrationExtensionsSpec{
			PreserveRawPayload: true,
		},
	}
}

// actionCatalog returns the v2.0.0 capability catalog: 14 callable ops + reactor.
// Naming follows docs/superpowers/specs/2026-05-27-yggdrasil-integration-capability-convention.md §7 (Tier C nfe.io).
// Each entry is also reflected in manifest/capability.*.yaml.
//
// Special cases retained:
//   - `retrieve_pdf` / `retrieve_xml` — kept (allowlist, helper-shaped — file URL retrieval).
//   - `manage_template` — kept (control-plane, not external resource).
//   - `bulk_issue` — kept (bulk action — documented special case per spec).
//   - `calculate_iss` — kept (allowlist — pure function).
func actionCatalog() []contract.IntegrationActionDefinition {
	return []contract.IntegrationActionDefinition{
		// service_invoice canonical triple (collapses issue_nfse / get_nfse_status / cancel_nfse).
		{Name: OpEnsureServiceInvoice, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Ensure a service invoice exists for the given external_id. POST /v2/companies/{id}/serviceinvoices; 409 duplicate decoded into existing invoice envelope (idempotent success)."},
		{Name: OpObserveServiceInvoices, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Read service invoices: with filter {id} returns one (GET /v2/companies/{id}/serviceinvoices/{id}); otherwise paginated GET /v2/companies/{id}/serviceinvoices."},
		{Name: OpDestroyServiceInvoice, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Cancel (destroy) an emitted NFSe. PUT /v2/companies/{id}/serviceinvoices/{id}/cancel. 422 cancellation_window_closed is terminal failure; 404 → already-absent success."},
		// File URL helpers (allowlisted).
		{Name: OpRetrievePDF, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Retrieve signed PDF download URL for an emitted NFSe (allowlisted helper)."},
		{Name: OpRetrieveXML, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Retrieve signed XML download URL for an emitted NFSe (allowlisted helper)."},
		// company canonical triple (collapses register_company).
		{Name: OpEnsureCompany, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceCompany}, Description: "Ensure a company exists at NFe.io for the given federal_tax_number. POSTs /v2/companies; 409 already_registered decoded into existing envelope (idempotent success)."},
		{Name: OpObserveCompanies, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceCompany}, Description: "Read companies registered at NFe.io. Filter by {federal_tax_number} or paginate list."},
		// municipality (read-only, renamed list_municipalities).
		{Name: OpObserveMunicipalities, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceMunicipality}, Description: "List municipalities supported by NFe.io. Cached 1h with stale-while-error fallback."},
		// Action-shaped helpers (kept by allowlist, see CLAUDE.md for rationale).
		{Name: OpManageTemplate, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceMunicipalityTemplate}, Description: "Read-only manage_template: get, list, validate operations on in-memory município templates (control-plane action, kept by allowlist)."},
		{Name: OpBulkIssue, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Bulk-issue up to 50 NFSe with semaphore 5 and partial-failure semantics (documented bulk-action special case, kept by allowlist)."},
		{Name: OpCalculateISS, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceMunicipalityTemplate}, Description: "Pure-function helper: compute ISS tax amount from municipality template and service amount (allowlisted)."},
		// webhook_subscription exact-ID lifecycle. Provider secrets and raw
		// configuration never enter observed resources or mutation events.
		{Name: OpEnsureWebhookSubscription, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceWebhookSubscription}, Description: "Reconcile a pre-existing NFe.io webhook by exact ID. Ordinary runs only set insecureSsl=false. An explicitly confirmed security migration may also set the provider HMAC from the runtime credential and remove legacy Authorization while preserving every other field."},
		{Name: OpObserveWebhookSubscriptions, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceWebhookSubscription}, Description: "Read one pre-existing webhook by exact ID (GET /v2/webhooks/{id}); enumeration and attribute-based adoption are disabled."},
		{Name: OpDestroyWebhookSubscription, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceWebhookSubscription}, Description: "Explicitly destroy one webhook by exact ID only when confirm_id equals id. DELETE /v2/webhooks/{id}; 404 is already-absent success."},
		// Reactor (NOT exposed via execute; triggered by inbound webhook HTTP server).
		{Name: ReactorWebhookReceived, Category: "reactor", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Reactor: receives NFe.io webhook, HMAC-verifies, dedupes, normalizes status, publishes to enterprise-payments.* queues via publish_message capability."},
	}
}
