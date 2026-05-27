package adapter

import (
	"context"
	"net/http"
	"testing"
)

func TestRegisterCompany_HappyPath(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/companies" {
			t.Errorf("method/path = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"cmp-1","federalTaxNumber":1,"name":"ACME","status":"Active"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)
	out, err := RegisterCompany(context.Background(), cli, RegisterCompanyInput{
		Name: "ACME", FederalTaxNumber: 1, Email: "x@y", TaxRegime: "simplesNacional",
		Address: map[string]any{}, LoginName: "u", LoginPassword: "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "cmp-1" || out.AlreadyRegistered {
		t.Fatalf("output mismatch: %+v", out)
	}
}

func TestRegisterCompany_409_AlreadyRegistered(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"id":"cmp-existing","federalTaxNumber":1,"name":"ACME","status":"Active"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)
	out, err := RegisterCompany(context.Background(), cli, RegisterCompanyInput{
		Name: "ACME", FederalTaxNumber: 1, Email: "x@y", TaxRegime: "simplesNacional",
		Address: map[string]any{}, LoginName: "u", LoginPassword: "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.AlreadyRegistered {
		t.Fatal("409 must set AlreadyRegistered=true")
	}
	if out.ID != "cmp-existing" {
		t.Fatalf("output.ID = %q; want cmp-existing decoded from 409 body", out.ID)
	}
}
