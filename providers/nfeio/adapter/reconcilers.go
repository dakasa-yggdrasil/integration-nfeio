package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	sdkadapter "github.com/dakasa-yggdrasil/yggdrasil-sdk-go/adapter"
	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/sdk/reconcile"
)

// This file wires the SDK v0.5.0 reconcile.RegisterReconciler dispatch for
// the three managed resource types in integration-nfeio v2.0.0:
//
//   - service_invoice    → ensure/observe/destroy + WithLegacyNames(issue_nfse, get_nfse_status, cancel_nfse, list_service_invoices)
//   - company            → ensure/observe/destroy + WithLegacyNames(register_company, update_company)
//   - webhook_subscription → ensure/observe/destroy + WithLegacyNames(create_webhook_endpoint, delete_webhook_endpoint, list_webhook_endpoints)
//
// The hand-written switch in executeRoute remains the primary dispatch path
// (it carries the legacy compat cases for direct RPC callers). This
// reconciler wiring is the recommended path for new callers — it produces
// the same JSON envelopes and respects the same idempotency contracts.
//
// Conformance with INTEGRATION_CONTRACT.md §5:
//   - Ensure: GET-then-PUT internally (or POST-with-409-adopt semantics).
//   - Observe: filter={id} routes to single-resource lookup, else paginated list.
//   - Destroy: 404 → success.

// serviceInvoiceReconciler bridges the existing hand-written handlers to the
// SDK Reconciler[D, O] interface. Desired = IssueNFSeInput; Observed =
// IssueNFSeOutput. The plan's worked example (Task 32 Step 1) names the
// types as serviceInvoiceDesired / serviceInvoiceObserved — those are
// aliases over the existing structs.
type serviceInvoiceDesired = IssueNFSeInput
type serviceInvoiceObserved = IssueNFSeOutput

type serviceInvoiceReconciler struct {
	cli       *Client
	templates map[string]*MunicipioTemplate
}

func newServiceInvoiceReconciler(cli *Client, templates map[string]*MunicipioTemplate) *serviceInvoiceReconciler {
	return &serviceInvoiceReconciler{cli: cli, templates: templates}
}

func (r *serviceInvoiceReconciler) Ensure(ctx context.Context, d serviceInvoiceDesired) (serviceInvoiceObserved, error) {
	out, err := IssueNFSe(ctx, r.cli, r.templates, d)
	if err != nil {
		return serviceInvoiceObserved{}, err
	}
	return *out, nil
}

func (r *serviceInvoiceReconciler) Observe(ctx context.Context, filter map[string]any) ([]serviceInvoiceObserved, string, error) {
	raw, _ := json.Marshal(filter)
	out, err := ObserveServiceInvoices(ctx, r.cli, raw)
	if err != nil {
		return nil, "", err
	}
	return out.Items, out.Cursor, nil
}

func (r *serviceInvoiceReconciler) Destroy(ctx context.Context, ref string) error {
	if ref == "" {
		return fmt.Errorf("destroy_service_invoice: ref (invoice_id) required")
	}
	_, err := CancelNFSe(ctx, r.cli, CancelNFSeInput{InvoiceID: ref})
	return err
}

// companyReconciler bridges RegisterCompany/EnsureCompany + ObserveCompanies
// into the SDK Reconciler shape.
type companyDesired = RegisterCompanyInput
type companyObserved = RegisterCompanyOutput

type companyReconciler struct {
	cli *Client
}

func newCompanyReconciler(cli *Client) *companyReconciler {
	return &companyReconciler{cli: cli}
}

func (r *companyReconciler) Ensure(ctx context.Context, d companyDesired) (companyObserved, error) {
	out, err := EnsureCompany(ctx, r.cli, d)
	if err != nil {
		return companyObserved{}, err
	}
	return *out, nil
}

func (r *companyReconciler) Observe(ctx context.Context, filter map[string]any) ([]companyObserved, string, error) {
	raw, _ := json.Marshal(filter)
	out, err := ObserveCompanies(ctx, r.cli, raw)
	if err != nil {
		return nil, "", err
	}
	return out.Items, out.Cursor, nil
}

func (r *companyReconciler) Destroy(ctx context.Context, ref string) error {
	// NFe.io v2 does not expose a company deletion endpoint; ensure_company
	// adoption is one-way. Returning a sentinel error matches the
	// pre-v2.0.0 behavior — destroy on this resource is informational.
	return fmt.Errorf("destroy_company: not supported by NFe.io v2 API")
}

// webhookSubReconciler wraps the new webhook_subscription resource.
type webhookSubDesired = EnsureWebhookSubscriptionInput
type webhookSubObserved = EnsureWebhookSubscriptionOutput

type webhookSubReconciler struct {
	cli *Client
}

func newWebhookSubReconciler(cli *Client) *webhookSubReconciler {
	return &webhookSubReconciler{cli: cli}
}

func (r *webhookSubReconciler) Ensure(ctx context.Context, d webhookSubDesired) (webhookSubObserved, error) {
	out, err := EnsureWebhookSubscription(ctx, r.cli, d)
	if err != nil {
		return webhookSubObserved{}, err
	}
	return *out, nil
}

func (r *webhookSubReconciler) Observe(ctx context.Context, filter map[string]any) ([]webhookSubObserved, string, error) {
	raw, _ := json.Marshal(filter)
	out, err := ObserveWebhookSubscriptions(ctx, r.cli, raw)
	if err != nil {
		return nil, "", err
	}
	return out.Items, out.Cursor, nil
}

func (r *webhookSubReconciler) Destroy(ctx context.Context, ref string) error {
	_, err := DestroyWebhookSubscription(ctx, r.cli, DestroyWebhookSubscriptionInput{ID: ref})
	return err
}

// WireReconcilers installs the v2.0.0 capability triples into the SDK
// reconcile dispatch table. Call once at startup, alongside the legacy
// Register("execute", ExecuteHandler(...)) chain — both dispatch paths
// produce equivalent JSON envelopes; the reconcile path is the recommended
// destination once callers migrate to canonical names.
//
// The WithLegacyNames entries here mirror Tier C from the rollout spec
// (docs/superpowers/specs/2026-05-27-yggdrasil-integration-capability-convention.md §7).
// They are removed at SDK v0.6.0; adapters dropping the shim should also
// remove the legacy cases in executeRoute.
func WireReconcilers(a *sdkadapter.Adapter, cli *Client, templates map[string]*MunicipioTemplate) {
	reconcile.RegisterReconciler[serviceInvoiceDesired, serviceInvoiceObserved](
		a, "service_invoice", "service_invoices",
		newServiceInvoiceReconciler(cli, templates),
		reconcile.WithLegacyNames("issue_nfse", "get_nfse_status", "cancel_nfse", "list_service_invoices"),
	)
	reconcile.RegisterReconciler[companyDesired, companyObserved](
		a, "company", "companies",
		newCompanyReconciler(cli),
		reconcile.WithLegacyNames("register_company", "update_company"),
	)
	reconcile.RegisterReconciler[webhookSubDesired, webhookSubObserved](
		a, "webhook_subscription", "webhook_subscriptions",
		newWebhookSubReconciler(cli),
		reconcile.WithLegacyNames("create_webhook_endpoint", "delete_webhook_endpoint", "list_webhook_endpoints"),
	)
}
