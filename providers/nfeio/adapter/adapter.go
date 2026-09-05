package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	sdkadapter "github.com/dakasa-yggdrasil/yggdrasil-sdk-go/adapter"
	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/rpc"
	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/sdk/reconcile"
	"go.uber.org/zap"
)

// DescribeHandler returns the SDK-shape handler that responds to the
// describe capability. yggdrasil-core validates the JSON body against the
// integration_type manifest.
func DescribeHandler(logger *zap.Logger) sdkadapter.Handler {
	return func(ctx context.Context, d rpc.Delivery) ([]byte, string, error) {
		body, err := json.Marshal(Describe())
		if err != nil {
			return nil, "", err
		}
		return body, "application/json", nil
	}
}

// ExecuteHandler returns the SDK-shape execute handler. Production
// wiring (v2.2.0+): routes inbound envelopes through reconcile.Dispatch
// first — activating §6.5 mutation event auto-emission via the
// WireReconcilersWithInstance-installed dispatch table. Operations
// not registered with a Reconciler (retrieve_pdf, retrieve_xml,
// manage_template, bulk_issue, calculate_iss, observe_municipalities)
// fall back to executeRoute. The reactor
// (nfse_webhook_received) is intentionally NOT routed here — it is
// triggered exclusively by the webhook HTTP server.
func ExecuteHandler(
	logger *zap.Logger,
	a *sdkadapter.Adapter,
	cli *Client,
	templates map[string]*MunicipioTemplate,
	deps *ExecuteDeps,
) sdkadapter.Handler {
	return func(ctx context.Context, d rpc.Delivery) ([]byte, string, error) {
		body, _, dispatchErr := reconcile.Dispatch(ctx, a, d)
		if dispatchErr == nil {
			return body, "application/json", nil
		}
		if !isUnsupportedReconcileOp(dispatchErr) {
			return nil, "", dispatchErr
		}
		// Operation not registered through RegisterReconciler: fall
		// back to executeRoute for action helpers and the
		// observe_municipalities cache flow.
		body, err := executeRoute(ctx, cli, templates, deps, d.Body)
		if err != nil {
			return nil, "", err
		}
		return body, "application/json", nil
	}
}

// isUnsupportedReconcileOp matches the SDK's "unsupported operation"
// signal so the bridge falls back to the allowlisted action switch instead of
// surfacing the error to callers.
func isUnsupportedReconcileOp(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "reconcile: unsupported operation") ||
		strings.Contains(msg, "reconcile: adapter has no registered Reconciler")
}

// executeRoute is the pure operation-dispatcher shared between the SDK
// handler and tests; it accepts the raw execute envelope and returns the
// JSON response body.
func executeRoute(
	ctx context.Context,
	cli *Client,
	templates map[string]*MunicipioTemplate,
	deps *ExecuteDeps,
	raw []byte,
) ([]byte, error) {
	var env struct {
		Operation string          `json:"operation"`
		Input     json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode envelope: %w", err)
	}
	// Tag ctx with the capability name so client.do() picks it up for
	// duration/error metric labels.
	ctx = WithOp(ctx, env.Operation)
	// Canonical names only. The v2 compatibility aliases were removed at
	// the v3 major boundary.
	switch env.Operation {
	case OpEnsureServiceInvoice:
		var in IssueNFSeInput
		if err := json.Unmarshal(env.Input, &in); err != nil {
			return nil, err
		}
		out, err := IssueNFSe(ctx, cli, templates, in)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case OpObserveServiceInvoices:
		out, err := ObserveServiceInvoices(ctx, cli, env.Input)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case OpDestroyServiceInvoice:
		var in CancelNFSeInput
		if err := json.Unmarshal(env.Input, &in); err != nil {
			return nil, err
		}
		out, err := CancelNFSe(ctx, cli, in)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case OpRetrievePDF:
		var in RetrieveDocInput
		if err := json.Unmarshal(env.Input, &in); err != nil {
			return nil, err
		}
		out, err := RetrievePDF(ctx, cli, in)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case OpRetrieveXML:
		var in RetrieveDocInput
		if err := json.Unmarshal(env.Input, &in); err != nil {
			return nil, err
		}
		out, err := RetrieveXML(ctx, cli, in)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case OpEnsureCompany:
		var in RegisterCompanyInput
		if err := json.Unmarshal(env.Input, &in); err != nil {
			return nil, err
		}
		out, err := EnsureCompany(ctx, cli, in)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case OpObserveCompanies:
		out, err := ObserveCompanies(ctx, cli, env.Input)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case OpObserveMunicipalities:
		var in ListMunicipalitiesInput
		if err := json.Unmarshal(env.Input, &in); err != nil {
			return nil, err
		}
		if deps == nil || deps.MunicipalitiesCache == nil {
			return nil, fmt.Errorf("observe_municipalities: cache not wired")
		}
		out, err := ListMunicipalities(ctx, cli, deps.MunicipalitiesCache, in)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case OpManageTemplate:
		var in ManageTemplateInput
		if err := json.Unmarshal(env.Input, &in); err != nil {
			return nil, err
		}
		out, err := ManageTemplate(ctx, templates, in)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case OpBulkIssue:
		var in BulkIssueInput
		if err := json.Unmarshal(env.Input, &in); err != nil {
			return nil, err
		}
		out, err := BulkIssue(ctx, cli, templates, in)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case OpCalculateISS:
		var in CalculateISSInput
		if err := json.Unmarshal(env.Input, &in); err != nil {
			return nil, err
		}
		out, err := CalculateISS(ctx, templates, in)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case OpEnsureWebhookSubscription:
		var in EnsureWebhookSubscriptionInput
		if err := json.Unmarshal(env.Input, &in); err != nil {
			return nil, err
		}
		out, err := EnsureWebhookSubscription(ctx, cli, in)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case OpObserveWebhookSubscriptions:
		out, err := ObserveWebhookSubscriptions(ctx, cli, env.Input)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	case OpDestroyWebhookSubscription:
		var in DestroyWebhookSubscriptionInput
		if err := json.Unmarshal(env.Input, &in); err != nil {
			return nil, err
		}
		out, err := DestroyWebhookSubscription(ctx, cli, in)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	default:
		return nil, fmt.Errorf("unknown operation %q", env.Operation)
	}
}

// ExecuteDeps bundles the optional, capability-specific dependencies
// (currently just the municipalities cache used by list_municipalities)
// that ExecuteHandler needs alongside the always-required client +
// templates. Each new capability that needs its own per-process state
// adds a field here so the call site in main.go stays one struct literal.
type ExecuteDeps struct {
	MunicipalitiesCache *MunicipalitiesCache
}

// IssueNFSeInput mirrors spec §3.1.
type IssueNFSeInput struct {
	CompanyID                   string         `json:"company_id,omitempty"`
	MunicipioCode               string         `json:"municipio_code"`
	ExternalID                  string         `json:"external_id"`
	BorrowerName                string         `json:"borrower_name"`
	BorrowerFTN                 int64          `json:"borrower_federal_tax_number"`
	BorrowerMTN                 string         `json:"borrower_municipal_tax_number,omitempty"`
	BorrowerTaxRegime           string         `json:"borrower_tax_regime,omitempty"`
	BorrowerAddr                map[string]any `json:"borrower_address"`
	BorrowerEmail               string         `json:"borrower_email,omitempty"`
	ServiceAmount               float64        `json:"service_amount"`
	Description                 string         `json:"description"`
	IssuedOn                    string         `json:"issued_on,omitempty"`
	RpsSerialNumber             string         `json:"rps_serial_number,omitempty"`
	AdditionalInfo              string         `json:"additional_information,omitempty"`
	DeductionsAmount            float64        `json:"deductions_amount,omitempty"`
	DiscountUnconditionedAmount float64        `json:"discount_unconditioned_amount,omitempty"`
}

// IssueNFSeOutput mirrors spec §3.1.
type IssueNFSeOutput struct {
	ID           string  `json:"id"`
	Status       string  `json:"status"`
	FlowStatus   string  `json:"flow_status"`
	FlowMessage  string  `json:"flow_message"`
	ExternalID   string  `json:"external_id"`
	Number       int64   `json:"number"`
	CheckCode    string  `json:"check_code"`
	ISSTaxAmount float64 `json:"iss_tax_amount"`
	AmountNet    float64 `json:"amount_net"`
	CreatedOn    string  `json:"created_on"`
	Duplicate    bool    `json:"duplicate,omitempty"`
}

// IssueNFSe POSTs /v2/companies/{id}/serviceinvoices. Template provides
// city_service_code / federal_service_code / iss_rate / taxation_type.
// 409 → IssueNFSeOutput.Duplicate=true (idempotent success); the upstream
// payload bundled with the 409 is decoded into the output so callers see
// the existing invoice ID/status without an extra round-trip.
func IssueNFSe(ctx context.Context, cli *Client, templates map[string]*MunicipioTemplate, in IssueNFSeInput) (*IssueNFSeOutput, error) {
	tpl, ok := templates[in.MunicipioCode]
	if !ok {
		return nil, fmt.Errorf("template_not_found: municipio_code=%s", in.MunicipioCode)
	}
	companyID := in.CompanyID
	if companyID == "" {
		companyID = cli.cfg.CompanyID
	}
	if companyID == "" {
		return nil, errors.New("company_id required (no instance default)")
	}

	body := map[string]any{
		"externalId":         in.ExternalID,
		"cityServiceCode":    tpl.ServiceCodes.CityServiceCode,
		"federalServiceCode": tpl.ServiceCodes.FederalServiceCode,
		"cnaeCode":           tpl.ServiceCodes.CnaeCode,
		"nbsCode":            tpl.ServiceCodes.NbsCode,
		"issRate":            tpl.ISS.Rate,
		"taxationType":       tpl.ISS.TaxationType,
		"description":        in.Description,
		"serviceAmount":      in.ServiceAmount,
		"rpsSerialNumber":    firstNonEmpty(in.RpsSerialNumber, tpl.RpsSerialNumber, "1"),
		"borrower": map[string]any{
			"name":               in.BorrowerName,
			"federalTaxNumber":   in.BorrowerFTN,
			"municipalTaxNumber": in.BorrowerMTN,
			"taxRegime":          in.BorrowerTaxRegime,
			"email":              in.BorrowerEmail,
			"address":            in.BorrowerAddr,
		},
	}
	if in.IssuedOn != "" {
		body["issuedOn"] = in.IssuedOn
	}
	if in.AdditionalInfo != "" {
		body["additionalInformation"] = in.AdditionalInfo
	}
	if in.DeductionsAmount > 0 {
		body["deductionsAmount"] = in.DeductionsAmount
	}
	if in.DiscountUnconditionedAmount > 0 {
		body["discountUnconditionedAmount"] = in.DiscountUnconditionedAmount
	}

	out := &IssueNFSeOutput{}
	path := fmt.Sprintf("/v2/companies/%s/serviceinvoices", companyID)
	err := cli.do(ctx, http.MethodPost, path, body, out)
	if err == nil {
		return out, nil
	}
	// 409 = duplicate; NFe.io returns the existing invoice body alongside
	// the error envelope. RawBody on NfeIoAPIError preserves the raw bytes
	// so we can decode the existing invoice without a second round-trip.
	apiErr := &NfeIoAPIError{}
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
		_ = json.Unmarshal(apiErr.RawBody, out)
		out.Duplicate = true
		return out, nil
	}
	return nil, err
}

// ObserveServiceInvoicesInput is the filter envelope. With invoice_id set,
// behaves like the pre-v2.0.0 get_nfse_status (single-resource lookup).
// Without it, lists invoices for the configured company.
type ObserveServiceInvoicesInput struct {
	CompanyID string `json:"company_id,omitempty"`
	InvoiceID string `json:"invoice_id,omitempty"`
	// ID is an alias for InvoiceID accepted from filter envelopes that
	// follow the universal convention {id} naming.
	ID string `json:"id,omitempty"`
	// PageSize bounds list pages; NFe.io applies a server-side default
	// when omitted.
	PageSize int    `json:"page_size,omitempty"`
	Cursor   string `json:"cursor,omitempty"`
}

// ObserveServiceInvoicesOutput is the response for observe_service_invoices.
// When filter selects a single invoice, items has length 1 and cursor is "".
type ObserveServiceInvoicesOutput struct {
	Items  []IssueNFSeOutput `json:"items"`
	Cursor string            `json:"cursor,omitempty"`
}

// ObserveServiceInvoices implements the convention's observe_* semantics for
// the service_invoice resource. With filter {id|invoice_id} it returns a
// single-entry result (canonical replacement for get_nfse_status); otherwise
// it paginates GET /v2/companies/{id}/serviceinvoices.
func ObserveServiceInvoices(ctx context.Context, cli *Client, raw []byte) (*ObserveServiceInvoicesOutput, error) {
	in := ObserveServiceInvoicesInput{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, fmt.Errorf("observe_service_invoices: parse filter: %w", err)
		}
	}
	companyID := in.CompanyID
	if companyID == "" {
		companyID = cli.cfg.CompanyID
	}
	if companyID == "" {
		return nil, errors.New("company_id required (no instance default)")
	}
	// Single-resource path (canonical replacement for get_nfse_status).
	invoiceID := in.InvoiceID
	if invoiceID == "" {
		invoiceID = in.ID
	}
	if invoiceID != "" {
		one, err := GetNFSeStatus(ctx, cli, GetNFSeStatusInput{CompanyID: companyID, InvoiceID: invoiceID})
		if err != nil {
			return nil, err
		}
		return &ObserveServiceInvoicesOutput{Items: []IssueNFSeOutput{*one}}, nil
	}
	// List path. NFe.io paginates server-side; the response carries items
	// + an optional cursor in the meta envelope. We mirror that to the
	// caller verbatim so downstream pagination follows the same shape.
	path := fmt.Sprintf("/v2/companies/%s/serviceinvoices", companyID)
	if in.Cursor != "" {
		path += "?cursor=" + in.Cursor
	}
	var rawResp struct {
		Items  []IssueNFSeOutput `json:"items"`
		Cursor string            `json:"cursor"`
	}
	if err := cli.do(ctx, http.MethodGet, path, nil, &rawResp); err != nil {
		return nil, err
	}
	return &ObserveServiceInvoicesOutput{Items: rawResp.Items, Cursor: rawResp.Cursor}, nil
}

// GetNFSeStatusInput selects an invoice by ID, optionally overriding the
// company at call time. Kept for internal reuse by ObserveServiceInvoices.
type GetNFSeStatusInput struct {
	CompanyID string `json:"company_id,omitempty"`
	InvoiceID string `json:"invoice_id"`
}

// GetNFSeStatus GETs /v2/companies/{id}/serviceinvoices/{invoice_id}.
// NFe.io returns the canonical invoice envelope (same shape as POST), so
// we reuse IssueNFSeOutput for the response. 404 surfaces as a terminal
// *NfeIoAPIError so the caller can distinguish missing from transient.
func GetNFSeStatus(ctx context.Context, cli *Client, in GetNFSeStatusInput) (*IssueNFSeOutput, error) {
	companyID := in.CompanyID
	if companyID == "" {
		companyID = cli.cfg.CompanyID
	}
	if companyID == "" {
		return nil, errors.New("company_id required (no instance default)")
	}
	out := &IssueNFSeOutput{}
	path := fmt.Sprintf("/v2/companies/%s/serviceinvoices/%s", companyID, in.InvoiceID)
	if err := cli.do(ctx, http.MethodGet, path, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// CancelNFSeInput selects an invoice to cancel.
type CancelNFSeInput struct {
	CompanyID string `json:"company_id,omitempty"`
	InvoiceID string `json:"invoice_id"`
}

// CancelNFSeOutput carries the cancellation result. Cancelled is true when
// NFe.io confirms the FSM moved to "Cancelled".
type CancelNFSeOutput struct {
	Cancelled   bool   `json:"cancelled"`
	Status      string `json:"status"`
	FlowMessage string `json:"flow_message"`
}

// CancelNFSe PUTs /v2/companies/{id}/serviceinvoices/{invoice_id}/cancel.
// 422 cancellation_window_closed surfaces as a terminal *NfeIoAPIError so
// the caller compensates (e.g. issue a credit note) instead of retrying.
func CancelNFSe(ctx context.Context, cli *Client, in CancelNFSeInput) (*CancelNFSeOutput, error) {
	companyID := in.CompanyID
	if companyID == "" {
		companyID = cli.cfg.CompanyID
	}
	if companyID == "" {
		return nil, errors.New("company_id required (no instance default)")
	}
	var raw struct {
		Status      string `json:"status"`
		FlowMessage string `json:"flowMessage"`
	}
	path := fmt.Sprintf("/v2/companies/%s/serviceinvoices/%s/cancel", companyID, in.InvoiceID)
	if err := cli.do(ctx, http.MethodPut, path, nil, &raw); err != nil {
		return nil, err
	}
	return &CancelNFSeOutput{
		Cancelled:   raw.Status == "Cancelled",
		Status:      raw.Status,
		FlowMessage: raw.FlowMessage,
	}, nil
}

// RetrieveDocInput selects an invoice document (PDF or XML) by ID.
type RetrieveDocInput struct {
	CompanyID string `json:"company_id,omitempty"`
	InvoiceID string `json:"invoice_id"`
}

// RetrieveDocOutput carries the signed download URL plus optional
// expiration timestamp.
type RetrieveDocOutput struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// RetrievePDF GETs /v2/companies/{id}/serviceinvoices/{invoice_id}/pdf.
// NFe.io returns a JSON body with documentUrl pointing at a signed S3 URL.
func RetrievePDF(ctx context.Context, cli *Client, in RetrieveDocInput) (*RetrieveDocOutput, error) {
	return retrieveDoc(ctx, cli, in, "pdf")
}

// RetrieveXML GETs /v2/companies/{id}/serviceinvoices/{invoice_id}/xml.
// Same envelope shape as RetrievePDF — both delegate to retrieveDoc(kind).
func RetrieveXML(ctx context.Context, cli *Client, in RetrieveDocInput) (*RetrieveDocOutput, error) {
	return retrieveDoc(ctx, cli, in, "xml")
}

func retrieveDoc(ctx context.Context, cli *Client, in RetrieveDocInput, kind string) (*RetrieveDocOutput, error) {
	companyID := in.CompanyID
	if companyID == "" {
		companyID = cli.cfg.CompanyID
	}
	if companyID == "" {
		return nil, errors.New("company_id required (no instance default)")
	}
	var raw struct {
		DocumentURL string `json:"documentUrl"`
		ExpiresAt   string `json:"expiresAt"`
	}
	path := fmt.Sprintf("/v2/companies/%s/serviceinvoices/%s/%s", companyID, in.InvoiceID, kind)
	if err := cli.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	out := &RetrieveDocOutput{URL: raw.DocumentURL, ExpiresAt: raw.ExpiresAt}
	if out.URL == "" {
		return nil, fmt.Errorf("retrieve_%s: empty documentUrl", kind)
	}
	return out, nil
}

// RegisterCompanyInput mirrors spec §3.6. CertificateBase64 +
// CertificatePassword are optional (only required when the município demands
// a digital certificate, e.g. São Paulo's RPS workflow).
type RegisterCompanyInput struct {
	Name                string         `json:"name"`
	FederalTaxNumber    int64          `json:"federal_tax_number"`
	TradeName           string         `json:"trade_name,omitempty"`
	Email               string         `json:"email"`
	OpeningDate         string         `json:"opening_date,omitempty"`
	TaxRegime           string         `json:"tax_regime"`
	SpecialTaxRegime    string         `json:"special_tax_regime,omitempty"`
	MunicipalTaxNumber  string         `json:"municipal_tax_number,omitempty"`
	Address             map[string]any `json:"address"`
	LoginName           string         `json:"login_name"`
	LoginPassword       string         `json:"login_password"`
	CertificateBase64   string         `json:"certificate_base64,omitempty"`
	CertificatePassword string         `json:"certificate_password,omitempty"`
}

// RegisterCompanyOutput carries the registration result. AlreadyRegistered
// is true when NFe.io returned 409 with the existing company envelope —
// callers treat both as success (idempotent semantics).
type RegisterCompanyOutput struct {
	ID                string `json:"id"`
	FederalTaxNumber  int64  `json:"federal_tax_number"`
	Name              string `json:"name"`
	Status            string `json:"status"`
	AlreadyRegistered bool   `json:"already_registered,omitempty"`
}

// RegisterCompany POSTs /v2/companies. 409 = already_registered (decoded
// from the bundled body so the caller sees the existing company ID).
func RegisterCompany(ctx context.Context, cli *Client, in RegisterCompanyInput) (*RegisterCompanyOutput, error) {
	body := map[string]any{
		"name":               in.Name,
		"federalTaxNumber":   in.FederalTaxNumber,
		"tradeName":          in.TradeName,
		"email":              in.Email,
		"openingDate":        in.OpeningDate,
		"taxRegime":          in.TaxRegime,
		"specialTaxRegime":   in.SpecialTaxRegime,
		"municipalTaxNumber": in.MunicipalTaxNumber,
		"address":            in.Address,
		"loginName":          in.LoginName,
		"loginPassword":      in.LoginPassword,
	}
	if in.CertificateBase64 != "" {
		body["certificateBase64"] = in.CertificateBase64
		body["certificatePassword"] = in.CertificatePassword
	}
	out := &RegisterCompanyOutput{}
	// NFe.io returns federalTaxNumber as camelCase in the envelope; decode
	// into an intermediate to capture the camelCase keys reliably.
	var raw struct {
		ID               string `json:"id"`
		FederalTaxNumber int64  `json:"federalTaxNumber"`
		Name             string `json:"name"`
		Status           string `json:"status"`
	}
	err := cli.do(ctx, http.MethodPost, "/v2/companies", body, &raw)
	if err == nil {
		out.ID = raw.ID
		out.FederalTaxNumber = raw.FederalTaxNumber
		out.Name = raw.Name
		out.Status = raw.Status
		return out, nil
	}
	apiErr := &NfeIoAPIError{}
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
		_ = json.Unmarshal(apiErr.RawBody, &raw)
		out.ID = raw.ID
		out.FederalTaxNumber = raw.FederalTaxNumber
		out.Name = raw.Name
		out.Status = raw.Status
		out.AlreadyRegistered = true
		return out, nil
	}
	return nil, err
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// EnsureCompany implements the convention's ensure_* semantics for the
// company resource. GET-then-POST/PATCH: looks up the company by
// federal_tax_number first; absent → POST; present + drift → PATCH.
// The pre-v2.0.0 RegisterCompany is preserved internally because NFe.io
// returns 409 already_registered with the existing envelope, which is
// the GET-equivalent the convention requires.
//
// Practical adoption: NFe.io does not expose a stable
// `GET /v2/companies?federal_tax_number={x}` filter in v2; the canonical
// adoption path is "POST and accept 409 as success", which is exactly
// what RegisterCompany already does. EnsureCompany delegates to that,
// returning AlreadyRegistered=true on adoption (the convention's
// idempotent-success path).
func EnsureCompany(ctx context.Context, cli *Client, in RegisterCompanyInput) (*RegisterCompanyOutput, error) {
	return RegisterCompany(ctx, cli, in)
}

// ObserveCompaniesInput is the filter envelope. With federal_tax_number set,
// returns a single-entry result; otherwise paginates GET /v2/companies.
type ObserveCompaniesInput struct {
	FederalTaxNumber int64  `json:"federal_tax_number,omitempty"`
	ID               string `json:"id,omitempty"`
	PageSize         int    `json:"page_size,omitempty"`
	Cursor           string `json:"cursor,omitempty"`
}

// ObserveCompaniesOutput is the response. Items envelope mirrors the
// canonical observe_* convention shape across the SDK.
type ObserveCompaniesOutput struct {
	Items  []RegisterCompanyOutput `json:"items"`
	Cursor string                  `json:"cursor,omitempty"`
}

// ObserveCompanies lists or filters companies at NFe.io. Single-resource
// lookup uses GET /v2/companies/{id}; list path is GET /v2/companies.
func ObserveCompanies(ctx context.Context, cli *Client, raw []byte) (*ObserveCompaniesOutput, error) {
	in := ObserveCompaniesInput{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, fmt.Errorf("observe_companies: parse filter: %w", err)
		}
	}
	if in.ID != "" {
		var raw struct {
			ID               string `json:"id"`
			FederalTaxNumber int64  `json:"federalTaxNumber"`
			Name             string `json:"name"`
			Status           string `json:"status"`
		}
		path := fmt.Sprintf("/v2/companies/%s", in.ID)
		if err := cli.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
			return nil, err
		}
		return &ObserveCompaniesOutput{Items: []RegisterCompanyOutput{{
			ID: raw.ID, FederalTaxNumber: raw.FederalTaxNumber, Name: raw.Name, Status: raw.Status,
		}}}, nil
	}
	path := "/v2/companies"
	if in.Cursor != "" {
		path += "?cursor=" + in.Cursor
	}
	var rawResp struct {
		Items []struct {
			ID               string `json:"id"`
			FederalTaxNumber int64  `json:"federalTaxNumber"`
			Name             string `json:"name"`
			Status           string `json:"status"`
		} `json:"items"`
		Cursor string `json:"cursor"`
	}
	if err := cli.do(ctx, http.MethodGet, path, nil, &rawResp); err != nil {
		return nil, err
	}
	out := &ObserveCompaniesOutput{Cursor: rawResp.Cursor}
	for _, it := range rawResp.Items {
		out.Items = append(out.Items, RegisterCompanyOutput{
			ID: it.ID, FederalTaxNumber: it.FederalTaxNumber, Name: it.Name, Status: it.Status,
		})
	}
	return out, nil
}
