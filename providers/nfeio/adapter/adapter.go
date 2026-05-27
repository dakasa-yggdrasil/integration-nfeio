package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"go.uber.org/zap"
)

// DescribeHandler returns the JSON-encoded Describe() response. The SDK
// wraps this in its envelope; the body shape is what yggdrasil-core
// validates against the integration_type manifest.
func DescribeHandler(logger *zap.Logger) func(ctx context.Context, raw []byte) ([]byte, error) {
	return func(ctx context.Context, raw []byte) ([]byte, error) {
		return json.Marshal(Describe())
	}
}

// ExecuteHandler returns the execute RPC handler. Routes by op name; the
// reactor (nfse_webhook_received) is intentionally NOT routed here — it is
// triggered exclusively by the webhook HTTP server.
func ExecuteHandler(
	logger *zap.Logger,
	cli *Client,
	templates map[string]*MunicipioTemplate,
	deps *ExecuteDeps,
) func(ctx context.Context, raw []byte) ([]byte, error) {
	return func(ctx context.Context, raw []byte) ([]byte, error) {
		var env struct {
			Operation string          `json:"operation"`
			Input     json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("decode envelope: %w", err)
		}
		switch env.Operation {
		case OpIssueNfse:
			var in IssueNFSeInput
			if err := json.Unmarshal(env.Input, &in); err != nil {
				return nil, err
			}
			out, err := IssueNFSe(ctx, cli, templates, in)
			if err != nil {
				return nil, err
			}
			return json.Marshal(out)
		case OpGetNfseStatus:
			var in GetNFSeStatusInput
			if err := json.Unmarshal(env.Input, &in); err != nil {
				return nil, err
			}
			out, err := GetNFSeStatus(ctx, cli, in)
			if err != nil {
				return nil, err
			}
			return json.Marshal(out)
		case OpCancelNfse:
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
		case OpRegisterCompany:
			var in RegisterCompanyInput
			if err := json.Unmarshal(env.Input, &in); err != nil {
				return nil, err
			}
			out, err := RegisterCompany(ctx, cli, in)
			if err != nil {
				return nil, err
			}
			return json.Marshal(out)
		case OpListMunicipalities:
			var in ListMunicipalitiesInput
			if err := json.Unmarshal(env.Input, &in); err != nil {
				return nil, err
			}
			if deps == nil || deps.MunicipalitiesCache == nil {
				return nil, fmt.Errorf("list_municipalities: cache not wired")
			}
			out, err := ListMunicipalities(ctx, cli, deps.MunicipalitiesCache, in)
			if err != nil {
				return nil, err
			}
			return json.Marshal(out)
		default:
			return nil, fmt.Errorf("unknown operation %q", env.Operation)
		}
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

// GetNFSeStatusInput selects an invoice by ID, optionally overriding the
// company at call time.
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
