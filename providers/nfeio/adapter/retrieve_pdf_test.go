package adapter

import (
	"context"
	"net/http"
	"testing"
)

func TestRetrievePDF_FromLocationHeader(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/companies/cmpDefault/serviceinvoices/inv-1/pdf" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Location", "https://s3.example/pdf/abc?sig=x")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"documentUrl":"https://s3.example/pdf/abc?sig=x"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)
	out, err := RetrievePDF(context.Background(), cli, RetrieveDocInput{InvoiceID: "inv-1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.URL == "" {
		t.Fatal("output.URL empty")
	}
}
