package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	sdkadapter "github.com/dakasa-yggdrasil/yggdrasil-sdk-go/adapter"
	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/sdk/events"
	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/sdk/reconcile"
)

// This file wires the SDK reconcile.RegisterReconciler dispatch for the three
// managed resource types in integration-nfeio v3:
//
//   - service_invoice: ensure/observe/destroy
//   - company: ensure/observe/destroy
//   - webhook_subscription: exact-ID ensure/observe plus guarded destroy
//
// The hand-written switch in executeRoute remains the dispatch path for
// allowlisted actions that are not resource reconcilers.
//
// Conformance with INTEGRATION_CONTRACT.md §5:
//   - Ensure: GET-then-PUT internally.
//   - Observe: filter={id} routes to a single-resource lookup.
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

// Destroy cancels the invoice named by ref under the instance default company.
func (r *serviceInvoiceReconciler) Destroy(ctx context.Context, ref string) error {
	return r.DestroyWithDesired(ctx, ref, serviceInvoiceDesired{})
}

// DestroyWithDesired is preferred by the SDK dispatch. It cancels the invoice
// named by ref under desired.CompanyID (the documented company_id input) and
// falls back to the instance default company when that is empty. The
// ExecuteHandler bridge copies the documented invoice_id input to ref.
//
// Success means NFe.io reported the invoice as Cancelled. Any other status
// returns a CancellationPendingError, so the SDK neither answers
// {"deleted":true} nor emits nfeio.service_invoice.destroyed while the
// fiscal document is still valid.
func (r *serviceInvoiceReconciler) DestroyWithDesired(ctx context.Context, ref string, desired serviceInvoiceDesired) error {
	if ref == "" {
		return fmt.Errorf("destroy_service_invoice: ref (invoice_id) required")
	}
	out, err := CancelNFSe(ctx, r.cli, CancelNFSeInput{CompanyID: desired.CompanyID, InvoiceID: ref})
	if err != nil {
		return err
	}
	if !out.Cancelled {
		return &CancellationPendingError{InvoiceID: ref, Status: out.Status, FlowMessage: out.FlowMessage}
	}
	return nil
}

// CancellationPendingCode is the code that prefixes a CancellationPendingError
// message, so callers that only see the error text can still match it.
const CancellationPendingCode = "cancellation_pending"

// maxProviderTextRunes bounds provider-supplied text copied into errors.
const maxProviderTextRunes = 200

// CancellationPendingError reports that NFe.io accepted a cancel request but
// has not confirmed the Cancelled state. NFe.io confirms cancellation
// asynchronously (the nfse.cancelled webhook), so the invoice is still a valid
// fiscal document. The error is retryable: calling destroy_service_invoice
// again later succeeds once NFe.io reports Cancelled. It carries only the
// invoice id and the provider's status and flow message, never credentials.
type CancellationPendingError struct {
	InvoiceID   string
	Status      string
	FlowMessage string
}

func (e *CancellationPendingError) Error() string {
	return fmt.Sprintf(`%s: destroy_service_invoice: NFe.io has not confirmed the cancellation of invoice "%s" (status "%s", flow_message "%s"); retry later`,
		CancellationPendingCode,
		providerText(e.InvoiceID),
		providerText(e.Status),
		providerText(e.FlowMessage),
	)
}

// Code returns CancellationPendingCode.
func (e *CancellationPendingError) Code() string { return CancellationPendingCode }

// Retryable reports that a later destroy_service_invoice call may succeed.
func (e *CancellationPendingError) Retryable() bool { return true }

// providerText makes provider-supplied text safe to embed in an error that
// the SDK wraps into a JSON body: it drops control characters, backslashes
// and double quotes, and truncates to maxProviderTextRunes.
func providerText(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '\\' || r == '"' {
			return -1
		}
		return r
	}, s)
	cleaned = strings.TrimSpace(cleaned)
	if runes := []rune(cleaned); len(runes) > maxProviderTextRunes {
		cleaned = string(runes[:maxProviderTextRunes]) + "..."
	}
	return cleaned
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

// webhookSubReconciler wraps exact-ID webhook reconciliation. The generic
// Destroy method is deliberately non-mutating; SDK callers must pass the full
// desired payload with an exact confirm_id through DestroyWithDesired.
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
	return out.Items, "", nil
}

func (r *webhookSubReconciler) Destroy(ctx context.Context, ref string) error {
	return fmt.Errorf("destroy_webhook_subscription: explicit confirm_id is required")
}

func (r *webhookSubReconciler) DestroyWithDesired(ctx context.Context, ref string, desired webhookSubDesired) error {
	if desired.Ref != "" && desired.Ref != ref {
		return fmt.Errorf("destroy_webhook_subscription: ref mismatch")
	}
	if desired.ID != "" && desired.ID != ref {
		return fmt.Errorf("destroy_webhook_subscription: id mismatch")
	}
	_, err := DestroyWebhookSubscription(ctx, r.cli, DestroyWebhookSubscriptionInput{
		ID:        ref,
		ConfirmID: desired.ConfirmID,
	})
	return err
}

// WireReconcilers installs the canonical capability triples into the SDK
// reconcile dispatch table. The one-minor v2 compatibility aliases are gone
// at the v3 major boundary.
func WireReconcilers(a *sdkadapter.Adapter, cli *Client, templates map[string]*MunicipioTemplate) {
	WireReconcilersWithInstance(a, cli, templates, "")
}

// WireReconcilersWithInstance is the v0.6.0 variant accepting an
// instanceID so emitted MutationEvents carry the multi-tenant scope.
// instanceID is only the fallback: the SDK prefers the top-level
// instance_id of each execute envelope, which ExecuteHandler lifts from
// Core's integration.instance.name (withEnvelopeInstance). Production
// passes "" so an envelope without an instance name yields an event with
// an empty InstanceID, which Core refuses (fail closed). The adapter does
// not bind the envelope's instance to its credentials: it serves every
// envelope with its one NFe.io credential set. Core's event principal
// grants only the production instance, so events naming another instance
// are refused.
//
// The emitter comes from the environment (newEmitterFromEnv).
func WireReconcilersWithInstance(a *sdkadapter.Adapter, cli *Client, templates map[string]*MunicipioTemplate, instanceID string) {
	wireReconcilers(a, cli, templates, instanceID, newEmitterFromEnv())
}

// wireReconcilers installs the reconcilers with an explicit emitter. It is
// the test seam: tests pass a capturing emitter to assert on the
// MutationEvents the SDK emits through the production ExecuteHandler.
func wireReconcilers(a *sdkadapter.Adapter, cli *Client, templates map[string]*MunicipioTemplate, instanceID string, emitter events.Emitter) {
	commonOpts := []reconcile.Option{
		reconcile.WithProvider(Provider),
		reconcile.WithEmitter(emitter),
		reconcile.WithInstanceID(instanceID),
	}

	reconcile.RegisterReconciler[serviceInvoiceDesired, serviceInvoiceObserved](
		a, "service_invoice", "service_invoices",
		newServiceInvoiceReconciler(cli, templates),
		commonOpts...,
	)
	reconcile.RegisterReconciler[companyDesired, companyObserved](
		a, "company", "companies",
		newCompanyReconciler(cli),
		commonOpts...,
	)
	reconcile.RegisterReconciler[webhookSubDesired, webhookSubObserved](
		a, "webhook_subscription", "webhook_subscriptions",
		newWebhookSubReconciler(cli),
		commonOpts...,
	)
}

// newEmitterFromEnv returns an events.Emitter wired to yggdrasil-core
// when YGGDRASIL_CORE_URL is set, otherwise a NoopEmitter. The HTTP
// emitter keeps the SDK defaults: it POSTs to YGGDRASIL_CORE_URL +
// /api/v1/events with YGGDRASIL_RUN_TOKEN as the bearer. That token is
// this adapter's own event publisher credential; the legacy publish
// dispatcher reads its own pair (PublishDispatcherFromEnv). Env-driven
// keeps the Lego principle (no broker / secret-store / cloud is
// hardcoded). Emission is best-effort per reconcile.WithEmitter
// docstring: failures log WARN but do not fail the capability call.
func newEmitterFromEnv() events.Emitter {
	if os.Getenv(events.EnvCoreURL) == "" {
		return &events.NoopEmitter{}
	}
	return events.NewHTTPEmitter()
}
