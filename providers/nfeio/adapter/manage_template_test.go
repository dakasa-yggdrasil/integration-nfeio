package adapter

import (
	"context"
	"testing"
)

func TestManageTemplate_Get(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	out, err := ManageTemplate(context.Background(), templates, ManageTemplateInput{
		Operation: "get", MunicipioCode: "3550308",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Template == nil || out.Template.MunicipioCode != "3550308" {
		t.Fatalf("output.Template mismatch: %+v", out.Template)
	}
}

func TestManageTemplate_List(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	out, err := ManageTemplate(context.Background(), templates, ManageTemplateInput{Operation: "list"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Templates) != 5 {
		t.Fatalf("len = %d; want 5", len(out.Templates))
	}
}

func TestManageTemplate_Validate_RejectsBadIssRate(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	bad := `apiVersion: nfeio.dakasa/v1
municipio_code: "0000000"
name: "Bad"
state: "ZZ"
service_codes:
  city_service_code: "x"
  federal_service_code: "y"
iss:
  rate: 2.0`
	out, err := ManageTemplate(context.Background(), templates, ManageTemplateInput{
		Operation: "validate", YAMLContent: bad,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Valid {
		t.Fatal("ISS rate 2.0 must fail validation")
	}
	if len(out.Errors) == 0 {
		t.Fatal("Errors must not be empty")
	}
}

func TestManageTemplate_Get_UnknownCode_TemplateNotFound(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	_, err := ManageTemplate(context.Background(), templates, ManageTemplateInput{
		Operation: "get", MunicipioCode: "9999999",
	})
	if err == nil {
		t.Fatal("unknown code must return template_not_found")
	}
}
