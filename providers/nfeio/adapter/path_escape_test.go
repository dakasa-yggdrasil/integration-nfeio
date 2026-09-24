package adapter

import (
	"context"
	"net/http"
	"testing"
)

// The ids below would add path segments, a query and a fragment if they were
// interpolated raw. Every NFe.io URL built from a company or invoice id must
// keep each one inside a single escaped path segment.
const (
	hostileCompanyID = "cmp/../other?x=1"
	hostileInvoiceID = "inv/1?y=2#frag"
	escapedCompanyID = "cmp%2F..%2Fother%3Fx=1"
	escapedInvoiceID = "inv%2F1%3Fy=2%23frag"
)

func TestNFeioPaths_EscapeCompanyAndInvoiceIDs(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	cases := []struct {
		name    string
		wantURI string
		reply   string
		call    func(cli *Client) error
	}{
		{
			name:    "ensure_service_invoice",
			wantURI: "/v2/companies/" + escapedCompanyID + "/serviceinvoices",
			reply:   `{"id":"inv-1","status":"Processing"}`,
			call: func(cli *Client) error {
				_, err := IssueNFSe(context.Background(), cli, templates, IssueNFSeInput{
					CompanyID: hostileCompanyID, MunicipioCode: "3550308", ExternalID: "ext-1",
					BorrowerName: "ACME", BorrowerFTN: 1,
					BorrowerAddr: map[string]any{}, ServiceAmount: 1, Description: "x",
				})
				return err
			},
		},
		{
			name:    "retrieve_pdf",
			wantURI: "/v2/companies/" + escapedCompanyID + "/serviceinvoices/" + escapedInvoiceID + "/pdf",
			reply:   `{"documentUrl":"https://example.invalid/doc.pdf"}`,
			call: func(cli *Client) error {
				_, err := RetrievePDF(context.Background(), cli, RetrieveDocInput{CompanyID: hostileCompanyID, InvoiceID: hostileInvoiceID})
				return err
			},
		},
		{
			name:    "retrieve_xml",
			wantURI: "/v2/companies/" + escapedCompanyID + "/serviceinvoices/" + escapedInvoiceID + "/xml",
			reply:   `{"documentUrl":"https://example.invalid/doc.xml"}`,
			call: func(cli *Client) error {
				_, err := RetrieveXML(context.Background(), cli, RetrieveDocInput{CompanyID: hostileCompanyID, InvoiceID: hostileInvoiceID})
				return err
			},
		},
		{
			name:    "destroy_service_invoice",
			wantURI: "/v2/companies/" + escapedCompanyID + "/serviceinvoices/" + escapedInvoiceID + "/cancel",
			reply:   `{"status":"Cancelled","flowMessage":""}`,
			call: func(cli *Client) error {
				_, err := CancelNFSe(context.Background(), cli, CancelNFSeInput{CompanyID: hostileCompanyID, InvoiceID: hostileInvoiceID})
				return err
			},
		},
		{
			name:    "observe_service_invoices",
			wantURI: "/v2/companies/" + escapedCompanyID + "/serviceinvoices/" + escapedInvoiceID,
			reply:   `{"id":"inv-1","status":"Issued"}`,
			call: func(cli *Client) error {
				_, err := GetNFSeStatus(context.Background(), cli, GetNFSeStatusInput{CompanyID: hostileCompanyID, InvoiceID: hostileInvoiceID})
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotURI, gotQuery string
			srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotURI = r.RequestURI
				gotQuery = r.URL.RawQuery
				_, _ = w.Write([]byte(tc.reply))
			})
			defer srv.Close()

			if err := tc.call(mustNewClient(t, srv.URL)); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if gotURI != tc.wantURI {
				t.Fatalf("request URI = %q; want %q", gotURI, tc.wantURI)
			}
			if gotQuery != "" {
				t.Fatalf("raw query = %q; an id must never add a query", gotQuery)
			}
		})
	}
}
