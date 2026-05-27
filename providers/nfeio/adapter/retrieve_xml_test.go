package adapter

import (
	"context"
	"net/http"
	"testing"
)

func TestRetrieveXML_HappyPath(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/companies/cmpDefault/serviceinvoices/inv-1/xml" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"documentUrl":"https://s3.example/xml/abc?sig=y"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)
	out, err := RetrieveXML(context.Background(), cli, RetrieveDocInput{InvoiceID: "inv-1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.URL == "" {
		t.Fatal("output.URL empty")
	}
}
