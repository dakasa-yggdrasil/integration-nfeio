package adapter

import (
	"context"
	"net/http"
	"testing"
)

func TestCancelNFSe_HappyPath(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s; want PUT", r.Method)
		}
		if r.URL.Path != "/v2/companies/cmpDefault/serviceinvoices/inv-1/cancel" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"Cancelled","flowMessage":"ok"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)
	out, err := CancelNFSe(context.Background(), cli, CancelNFSeInput{InvoiceID: "inv-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Cancelled || out.Status != "Cancelled" {
		t.Fatalf("output mismatch: %+v", out)
	}
}

func TestCancelNFSe_422_WindowClosed_Terminal(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"status":422,"name":"cancellation_window_closed","message":"window expired"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)
	_, err := CancelNFSe(context.Background(), cli, CancelNFSeInput{InvoiceID: "x"})
	if err == nil {
		t.Fatal("422 must error")
	}
}
