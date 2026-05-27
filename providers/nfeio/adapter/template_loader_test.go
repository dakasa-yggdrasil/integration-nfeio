package adapter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTemplateLoader_LoadDir_Valid5Municipios(t *testing.T) {
	// Loads the 5 templates produced by Task 17. Tests run from package
	// dir so the relative path is `../../../manifest/templates`.
	dir := filepath.Join("..", "..", "..", "manifest", "templates")
	got, err := LoadTemplatesDir(dir)
	if err != nil {
		t.Fatalf("LoadTemplatesDir err = %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("len(templates) = %d; want 5", len(got))
	}

	want := []string{"3550308", "3304557", "4106902", "4205407", "3106200"}
	for _, code := range want {
		tpl, ok := got[code]
		if !ok {
			t.Errorf("template for IBGE %s missing", code)
			continue
		}
		if tpl.MunicipioCode != code {
			t.Errorf("template[%s].MunicipioCode = %q", code, tpl.MunicipioCode)
		}
		if tpl.Name == "" || tpl.State == "" {
			t.Errorf("template[%s] missing required Name/State", code)
		}
		if tpl.ISS.Rate <= 0 || tpl.ISS.Rate > 1 {
			t.Errorf("template[%s].ISS.Rate = %f; must be between 0 and 1", code, tpl.ISS.Rate)
		}
		if tpl.ServiceCodes.CityServiceCode == "" || tpl.ServiceCodes.FederalServiceCode == "" {
			t.Errorf("template[%s] missing required service codes", code)
		}
	}
}

func TestTemplateLoader_LoadDir_MissingRequiredField_FailsFast(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "9999999.yaml")
	if err := os.WriteFile(bad, []byte("municipio_code: \"9999999\"\nname: \"Bad\""), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadTemplatesDir(dir)
	if err == nil {
		t.Fatal("LoadTemplatesDir must error on missing required state/iss.rate/service_codes")
	}
}
