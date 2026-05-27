package adapter

import (
	"context"
	"testing"
)

func TestCalculateISS_SaoPaulo_2pct(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	out, err := CalculateISS(context.Background(), templates, CalculateISSInput{
		MunicipioCode: "3550308",
		ServiceAmount: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ISSRate != 0.02 || out.BaseTaxAmount != 1000 || out.ISSTaxAmount != 20 {
		t.Fatalf("output mismatch: %+v", out)
	}
}

func TestCalculateISS_Floripa_5pct_WithDeduction(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	out, err := CalculateISS(context.Background(), templates, CalculateISSInput{
		MunicipioCode: "4205407",
		ServiceAmount: 1000,
		Deductions:    100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.BaseTaxAmount != 900 || out.ISSTaxAmount != 45 {
		t.Fatalf("base=%f iss=%f; want 900 and 45", out.BaseTaxAmount, out.ISSTaxAmount)
	}
}

func TestCalculateISS_RateOverride_Honored(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	out, err := CalculateISS(context.Background(), templates, CalculateISSInput{
		MunicipioCode:   "3550308",
		ServiceAmount:   1000,
		ISSRateOverride: 0.03,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ISSRate != 0.03 || out.ISSTaxAmount != 30 {
		t.Fatalf("override not honored: %+v", out)
	}
}

func TestCalculateISS_UnknownMunicipio_TemplateNotFound(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	_, err := CalculateISS(context.Background(), templates, CalculateISSInput{
		MunicipioCode: "9999999", ServiceAmount: 100,
	})
	if err == nil {
		t.Fatal("unknown code must return template_not_found")
	}
}
