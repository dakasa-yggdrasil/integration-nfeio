package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestIssueNFSe_HappyPath_3550308(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s; want POST", r.Method)
		}
		if r.URL.Path != "/v2/companies/cmpDefault/serviceinvoices" {
			t.Errorf("path = %s", r.URL.Path)
		}
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		// Template-sourced values must be populated.
		if body["issRate"] != 0.02 {
			t.Errorf("issRate = %v; want 0.02 (SP)", body["issRate"])
		}
		if body["cityServiceCode"] != "10.08 / 171220001" {
			t.Errorf("cityServiceCode = %v", body["cityServiceCode"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"inv-1","status":"Processing","externalId":"ext-1","flowStatus":"WaitingSendToAuthorize"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)

	out, err := IssueNFSe(context.Background(), cli, templates, IssueNFSeInput{
		MunicipioCode: "3550308",
		ExternalID:    "ext-1",
		BorrowerName:  "ACME LTDA",
		BorrowerFTN:   12345678000100,
		BorrowerAddr: map[string]any{
			"street": "X", "number": "1", "district": "Y",
			"city_code": "3550308", "city_name": "São Paulo", "state": "SP",
			"postal_code": "01000-000", "country": "BRA",
		},
		ServiceAmount: 1000.0,
		Description:   "Hosting service",
	})
	if err != nil {
		t.Fatalf("IssueNFSe err = %v", err)
	}
	if out.ID != "inv-1" || out.Status != "Processing" {
		t.Fatalf("output mismatch: %+v", out)
	}
}

func TestIssueNFSe_409_DuplicateMarkedInMetadata(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"id":"inv-existing","status":"Issued","externalId":"ext-dup"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)
	out, err := IssueNFSe(context.Background(), cli, templates, IssueNFSeInput{
		MunicipioCode: "3550308", ExternalID: "ext-dup",
		BorrowerName: "ACME", BorrowerFTN: 1,
		BorrowerAddr: map[string]any{}, ServiceAmount: 1, Description: "x",
	})
	if err != nil {
		t.Fatalf("409 must NOT be a transport error: %v", err)
	}
	if !out.Duplicate {
		t.Errorf("output.Duplicate = false; want true on 409")
	}
	if out.ID != "inv-existing" {
		t.Errorf("output.ID = %q; want inv-existing decoded from 409 body", out.ID)
	}
}

func mustLoadTestTemplates(t *testing.T) map[string]*MunicipioTemplate {
	t.Helper()
	got, err := LoadTemplatesDir("../../../manifest/templates")
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	return got
}
