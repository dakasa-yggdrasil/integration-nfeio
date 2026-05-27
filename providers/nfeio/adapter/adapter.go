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
//
// MunicipalitiesCache is wired in Task 24 alongside list_municipalities.
type ExecuteDeps struct {
	MunicipalitiesCache any
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
