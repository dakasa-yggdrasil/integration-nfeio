package adapter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// BulkIssueInput batches up to 50 issue_nfse calls. CompanyID, when set,
// overrides each item's CompanyID (per-item override still wins). FailFast
// is reserved — v1.0.0 always runs to completion.
type BulkIssueInput struct {
	CompanyID string           `json:"company_id,omitempty"`
	Items     []IssueNFSeInput `json:"items"`
	FailFast  bool             `json:"fail_fast,omitempty"`
}

// BulkIssueResult is one per-item outcome in the result array. Order
// matches the input order so callers can correlate via Index.
type BulkIssueResult struct {
	Index        int    `json:"index"`
	ExternalID   string `json:"external_id"`
	Success      bool   `json:"success"`
	InvoiceID    string `json:"invoice_id,omitempty"`
	Status       string `json:"status,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// BulkIssueOutput aggregates the batch result. SucceededCount + FailedCount
// always sum to len(Results); a partial-failure batch is NOT an error from
// the caller's perspective (the slice carries per-item status).
type BulkIssueOutput struct {
	Results        []BulkIssueResult `json:"results"`
	SucceededCount int               `json:"succeeded_count"`
	FailedCount    int               `json:"failed_count"`
}

// BulkIssue invokes IssueNFSe for each item with a concurrency cap of 5.
// > 50 items returns terminal input_too_large. Per-item failures populate
// ErrorCode (when a NfeIoAPIError is returned) and never abort the batch.
func BulkIssue(ctx context.Context, cli *Client, templates map[string]*MunicipioTemplate, in BulkIssueInput) (*BulkIssueOutput, error) {
	if len(in.Items) > 50 {
		return nil, fmt.Errorf("input_too_large: %d items (max 50)", len(in.Items))
	}
	if len(in.Items) == 0 {
		return &BulkIssueOutput{Results: []BulkIssueResult{}}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	sem := make(chan struct{}, 5)
	results := make([]BulkIssueResult, len(in.Items))
	wg := sync.WaitGroup{}
	for i, item := range in.Items {
		i, item := i, item
		if in.CompanyID != "" && item.CompanyID == "" {
			item.CompanyID = in.CompanyID
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			itemCtx, itemCancel := context.WithTimeout(ctx, 15*time.Second)
			defer itemCancel()
			// Each item still routes through issue_nfse for client metrics;
			// the bulk_issue label is tracked via metricBulkIssueItems.
			itemCtx = WithOp(itemCtx, OpIssueNfse)
			out, err := IssueNFSe(itemCtx, cli, templates, item)
			r := BulkIssueResult{Index: i, ExternalID: item.ExternalID}
			if err != nil {
				r.Success = false
				r.ErrorMessage = err.Error()
				apiErr := &NfeIoAPIError{}
				if errors.As(err, &apiErr) {
					r.ErrorCode = apiErr.Name
				}
				metricBulkIssueItems.WithLabelValues("error").Inc()
			} else {
				r.Success = true
				r.InvoiceID = out.ID
				r.Status = out.Status
				metricBulkIssueItems.WithLabelValues("success").Inc()
			}
			results[i] = r
		}()
	}
	wg.Wait()
	out := &BulkIssueOutput{Results: results}
	for _, r := range results {
		if r.Success {
			out.SucceededCount++
		} else {
			out.FailedCount++
		}
	}
	return out, nil
}
