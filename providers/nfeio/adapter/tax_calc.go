package adapter

import (
	"context"
	"fmt"
)

// CalculateISSInput requests an ISS calculation for one município.
// ISSRateOverride wins over the template's default when > 0. Deductions
// are subtracted from ServiceAmount to derive the tax base.
type CalculateISSInput struct {
	MunicipioCode   string  `json:"municipio_code"`
	ServiceAmount   float64 `json:"service_amount"`
	ISSRateOverride float64 `json:"iss_rate_override,omitempty"`
	Deductions      float64 `json:"deductions_amount,omitempty"`
}

// CalculateISSOutput carries the resolved tax. ISSRate is the effective
// rate (template default or override). ISSTaxAmount = BaseTaxAmount * Rate.
type CalculateISSOutput struct {
	ISSRate       float64 `json:"iss_rate"`
	BaseTaxAmount float64 `json:"base_tax_amount"`
	ISSTaxAmount  float64 `json:"iss_tax_amount"`
}

// CalculateISS is the pure-function sub-capability defined in spec §3-extra.
// No network call; rate sourced from in-memory template map. Override
// honored when provided. base = max(0, ServiceAmount - Deductions).
func CalculateISS(ctx context.Context, templates map[string]*MunicipioTemplate, in CalculateISSInput) (*CalculateISSOutput, error) {
	tpl, ok := templates[in.MunicipioCode]
	if !ok {
		return nil, fmt.Errorf("template_not_found: %s", in.MunicipioCode)
	}
	rate := tpl.ISS.Rate
	if in.ISSRateOverride > 0 {
		rate = in.ISSRateOverride
	}
	base := in.ServiceAmount - in.Deductions
	if base < 0 {
		base = 0
	}
	return &CalculateISSOutput{
		ISSRate:       rate,
		BaseTaxAmount: base,
		ISSTaxAmount:  base * rate,
	}, nil
}
