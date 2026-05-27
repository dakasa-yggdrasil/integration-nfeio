// Package contract defines the wire-shape JSON types the adapter exchanges
// with yggdrasil-core during describe/execute. Mirrors the canonical
// shape published by integration-template/internal/protocol so consumers
// (yggdrasil-core describe validator + this adapter's spec.go) interop.
package contract

// AdapterDescribeResponse is the shape returned by Describe(). Matches
// integration-template/internal/protocol.AdapterDescribeResponse.
type AdapterDescribeResponse struct {
	Provider         string                        `json:"provider"`
	Adapter          IntegrationAdapterSpec        `json:"adapter"`
	Capabilities     []string                      `json:"capabilities"`
	CredentialSchema IntegrationSchemaSpec         `json:"credential_schema"`
	InstanceSchema   IntegrationSchemaSpec         `json:"instance_schema"`
	ResourceTypes    []IntegrationResourceType     `json:"resource_types"`
	ActionCatalog    []IntegrationActionDefinition `json:"action_catalog,omitempty"`
	Discovery        IntegrationDiscoverySpec      `json:"discovery"`
	Normalization    IntegrationNormalizationSpec  `json:"normalization"`
	Execution        IntegrationExecutionSpec      `json:"execution"`
	Extensions       IntegrationExtensionsSpec     `json:"extensions"`
}

// IntegrationAdapterSpec describes the adapter binary itself.
type IntegrationAdapterSpec struct {
	Transport      string                  `json:"transport"`
	Version        string                  `json:"version"`
	Queues         IntegrationAdapterQueue `json:"queues,omitempty"`
	Endpoints      IntegrationAdapterRoute `json:"endpoints,omitempty"`
	TimeoutSeconds int                     `json:"timeout_seconds,omitempty"`
}

// IntegrationAdapterQueue carries the queue names yggdrasil-core publishes to
// when transport=rabbitmq.
type IntegrationAdapterQueue struct {
	Describe string `json:"describe,omitempty"`
	Discover string `json:"discover,omitempty"`
	Read     string `json:"read,omitempty"`
	Execute  string `json:"execute,omitempty"`
	Sync     string `json:"sync,omitempty"`
	Health   string `json:"health,omitempty"`
}

// IntegrationAdapterRoute carries the HTTP paths yggdrasil-core POSTs to
// when transport=http_json.
type IntegrationAdapterRoute struct {
	Describe string `json:"describe,omitempty"`
	Execute  string `json:"execute,omitempty"`
}

// IntegrationSchemaSpec describes credential or instance configuration shape.
type IntegrationSchemaSpec struct {
	Mode       string                               `json:"mode"`
	Required   []string                             `json:"required,omitempty"`
	Properties map[string]IntegrationSchemaProperty `json:"properties,omitempty"`
}

// IntegrationSchemaProperty is one field in a schema spec.
type IntegrationSchemaProperty struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Enum        []any  `json:"enum,omitempty"`
	Default     any    `json:"default,omitempty"`
}

// IntegrationResourceType declares one resource the adapter operates on.
type IntegrationResourceType struct {
	Name             string   `json:"name"`
	CanonicalPrefix  string   `json:"canonical_prefix"`
	IdentityTemplate string   `json:"identity_template"`
	Discoverable     bool     `json:"discoverable"`
	DefaultActions   []string `json:"default_actions"`
}

// IntegrationActionDefinition is one entry in the action catalog. Reactor
// entries set Category="reactor" so yggdrasil-core hides them from grant
// pickers.
type IntegrationActionDefinition struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	ResourceTypes []string `json:"resource_types,omitempty"`
	Idempotent    bool     `json:"idempotent,omitempty"`
	Category      string   `json:"category,omitempty"`
}

// IntegrationDiscoverySpec describes how resources are discovered.
type IntegrationDiscoverySpec struct {
	Mode             string `json:"mode"`
	Cursor           string `json:"cursor,omitempty"`
	SupportsWebhooks bool   `json:"supports_webhooks,omitempty"`
}

// IntegrationNormalizationSpec describes how raw resources are normalized.
type IntegrationNormalizationSpec struct {
	ExternalIDPath         string `json:"external_id_path"`
	NamePath               string `json:"name_path,omitempty"`
	OwnerPath              string `json:"owner_path,omitempty"`
	FallbackResourcePrefix string `json:"fallback_resource_prefix"`
}

// IntegrationExecutionSpec carries execute-side metadata.
type IntegrationExecutionSpec struct {
	SupportsDryRun    bool     `json:"supports_dry_run,omitempty"`
	IdempotentActions []string `json:"idempotent_actions,omitempty"`
}

// IntegrationExtensionsSpec carries opt-in feature flags.
type IntegrationExtensionsSpec struct {
	AllowCustomResourceTypes bool `json:"allow_custom_resource_types,omitempty"`
	AllowCustomActions       bool `json:"allow_custom_actions,omitempty"`
	PreserveRawPayload       bool `json:"preserve_raw_payload,omitempty"`
}

// Action is a friendlier alias used by the adapter's local actionCatalog()
// helper. The wire-level type is IntegrationActionDefinition.
type Action = IntegrationActionDefinition
