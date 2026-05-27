package adapter

import (
	"context"
	"net/http"
	"testing"
)

func TestGetNFSeStatus_HappyPath_Issued(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s; want GET", r.Method)
		}
		if r.URL.Path != "/v2/companies/cmpDefault/serviceinvoices/inv-1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"inv-1","status":"Issued","number":42,"checkCode":"abcd"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)
	out, err := GetNFSeStatus(context.Background(), cli, GetNFSeStatusInput{InvoiceID: "inv-1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "Issued" || out.Number != 42 {
		t.Fatalf("output mismatch: %+v", out)
	}
}

func TestGetNFSeStatus_404_Terminal(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":404,"name":"not_found","message":"invoice not found"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)
	_, err := GetNFSeStatus(context.Background(), cli, GetNFSeStatusInput{InvoiceID: "x"})
	if err == nil {
		t.Fatal("404 must return error")
	}
}
