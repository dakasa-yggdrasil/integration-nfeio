package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestBulkIssue_PartialFailure_4Of50(t *testing.T) {
	var counter int32
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&counter, 1)
		// Fail items 1, 5, 9, 13 (4 failures)
		if n == 1 || n == 5 || n == 9 || n == 13 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"status":422,"name":"reject","message":"bad"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ext, _ := body["externalId"].(string)
		_, _ = w.Write([]byte(`{"id":"inv-` + ext + `","status":"Processing","externalId":"` + ext + `"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)
	templates := mustLoadTestTemplates(t)

	items := make([]IssueNFSeInput, 50)
	for i := range items {
		items[i] = IssueNFSeInput{
			MunicipioCode: "3550308",
			ExternalID:    fmt.Sprintf("ext-%d", i),
			BorrowerName:  "X", BorrowerFTN: 1,
			BorrowerAddr: map[string]any{}, ServiceAmount: 1, Description: "x",
		}
	}
	out, err := BulkIssue(context.Background(), cli, templates, BulkIssueInput{Items: items})
	if err != nil {
		t.Fatalf("bulk_issue must NOT return transport error on partial fail: %v", err)
	}
	if out.SucceededCount+out.FailedCount != 50 {
		t.Fatalf("totals don't add up: %d + %d", out.SucceededCount, out.FailedCount)
	}
	if out.FailedCount != 4 {
		t.Fatalf("failed = %d; want 4 (items 1,5,9,13)", out.FailedCount)
	}
	if len(out.Results) != 50 {
		t.Fatalf("len(results) = %d; want 50", len(out.Results))
	}
}

func TestBulkIssue_TooLarge_Terminal(t *testing.T) {
	cli := mustNewClient(t, "http://invalid")
	templates := mustLoadTestTemplates(t)
	items := make([]IssueNFSeInput, 51)
	_, err := BulkIssue(context.Background(), cli, templates, BulkIssueInput{Items: items})
	if err == nil {
		t.Fatal("> 50 items must be terminal input_too_large")
	}
}
