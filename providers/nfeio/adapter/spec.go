package adapter

import (
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/dakasa-yggdrasil/integration-nfeio/family/contract"
)

// Prometheus metrics shared across the adapter. Capability-side metrics
// (request duration, error counter, bulk_issue item counter, template_load)
// land in the observability finalization pass (Task 31); this initial set
// unblocks the client retry path + the webhook server.
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
)

func init() {
	prometheus.MustRegister(metricRateLimitRemaining, metricWebhookReceived, metricDedupHits)
}

const (
	// Provider is the family id. Matches the integration_type spec.provider
	// field that yggdrasil-core sends in the describe handshake.
	Provider = "nfeio"
	// IntegrationType is the per-provider id. SDK uses it to derive the AMQP
	// queue prefix yggdrasil.adapter.nfeio.{describe,execute}.
	IntegrationType = "nfeio"
	AdapterVersion  = "1.0.0"

	OpIssueNfse          = "issue_nfse"
	OpGetNfseStatus      = "get_nfse_status"
	OpCancelNfse         = "cancel_nfse"
	OpRetrievePDF        = "retrieve_pdf"
	OpRetrieveXML        = "retrieve_xml"
	OpRegisterCompany    = "register_company"
	OpListMunicipalities = "list_municipalities"
	OpManageTemplate     = "manage_template"
	OpBulkIssue          = "bulk_issue"
	OpCalculateISS       = "calculate_iss"

	ReactorWebhookReceived = "nfse_webhook_received"

	QueueDescribe = "yggdrasil.adapter.nfeio.describe"
	QueueExecute  = "yggdrasil.adapter.nfeio.execute"

	ResourceServiceInvoice      = "service_invoice"
	ResourceCompany             = "company"
	ResourceMunicipality        = "municipality"
	ResourceMunicipalityTemplate = "municipality_template"
)

// SupportedExecuteOperations lists the operations callable via the execute
// RPC path. The reactor (nfse_webhook_received) is intentionally excluded —
// it is triggered by the webhook HTTP server, not RPC.
var SupportedExecuteOperations = []string{
	OpIssueNfse, OpGetNfseStatus, OpCancelNfse, OpRetrievePDF, OpRetrieveXML,
	OpRegisterCompany, OpListMunicipalities, OpManageTemplate, OpBulkIssue,
	OpCalculateISS,
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
			Mode:     "env",
			Required: []string{"NFEIO_API_KEY", "NFEIO_WEBHOOK_SECRET"},
		},
		InstanceSchema: contract.IntegrationSchemaSpec{
			Mode: "inline",
		},
		ResourceTypes: []contract.IntegrationResourceType{
			{
				Name:             ResourceServiceInvoice,
				CanonicalPrefix:  "thirdparty.nfeio.service_invoice",
				IdentityTemplate: "service_invoice.{external_id}",
				Discoverable:     false,
				DefaultActions: []string{
					OpIssueNfse, OpGetNfseStatus, OpCancelNfse,
					OpRetrievePDF, OpRetrieveXML, OpBulkIssue,
				},
			},
			{
				Name:             ResourceCompany,
				CanonicalPrefix:  "thirdparty.nfeio.company",
				IdentityTemplate: "company.{federal_tax_number}",
				Discoverable:     false,
				DefaultActions:   []string{OpRegisterCompany},
			},
			{
				Name:             ResourceMunicipality,
				CanonicalPrefix:  "thirdparty.nfeio.municipality",
				IdentityTemplate: "municipality.{code}",
				Discoverable:     false,
				DefaultActions:   []string{OpListMunicipalities},
			},
			{
				Name:             ResourceMunicipalityTemplate,
				CanonicalPrefix:  "thirdparty.nfeio.municipality_template",
				IdentityTemplate: "municipality_template.{code}",
				Discoverable:     false,
				DefaultActions:   []string{OpManageTemplate, OpCalculateISS},
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

// actionCatalog returns the 10 capabilities + reactor that describe wires up.
// Each entry is also reflected in manifest/capability.*.yaml.
func actionCatalog() []contract.IntegrationActionDefinition {
	return []contract.IntegrationActionDefinition{
		{Name: OpIssueNfse, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Emit a NFSe via POST /v2/companies/{id}/serviceinvoices. Deduplicates on external_id. Municipality template provides ISS rate and service codes. 409 → duplicate=true."},
		{Name: OpGetNfseStatus, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Fetch current status of an emitted NFSe."},
		{Name: OpCancelNfse, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Cancel an emitted NFSe. 422 cancellation_window_closed is terminal."},
		{Name: OpRetrievePDF, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Retrieve PDF download URL for an emitted NFSe."},
		{Name: OpRetrieveXML, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Retrieve XML download URL for an emitted NFSe."},
		{Name: OpRegisterCompany, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceCompany}, Description: "Register a company in NFe.io. 409 already_registered is idempotent success."},
		{Name: OpListMunicipalities, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceMunicipality}, Description: "List municipalities supported by NFe.io. Cached 1h with stale-while-error fallback."},
		{Name: OpManageTemplate, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceMunicipalityTemplate}, Description: "Read-only manage_template: get, list, validate operations on in-memory município templates."},
		{Name: OpBulkIssue, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Bulk-issue up to 50 NFSe with semaphore 5 and partial-failure semantics."},
		{Name: OpCalculateISS, Category: "capability", Idempotent: true, ResourceTypes: []string{ResourceMunicipalityTemplate}, Description: "Pure-function: compute ISS tax amount from municipality template and service amount."},
		{Name: ReactorWebhookReceived, Category: "reactor", Idempotent: true, ResourceTypes: []string{ResourceServiceInvoice}, Description: "Reactor: receives NFe.io webhook, dedupes, normalizes status, publishes to enterprise-payments.* queues."},
	}
}
