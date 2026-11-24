package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ad "github.com/dakasa-yggdrasil/integration-nfeio/providers/nfeio/adapter"
	"github.com/dakasa-yggdrasil/integration-nfeio/providers/nfeio/config"
	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/adapter"
	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/rpc"
	sdkhttp "github.com/dakasa-yggdrasil/yggdrasil-sdk-go/rpc/http"
	"go.uber.org/zap"
)

type originalRunObserver struct {
	*adapter.Adapter
	runReturned chan error
	waitEntered chan struct{}
	waitCalls   atomic.Int32
}

func (r *originalRunObserver) Run(ctx context.Context) error {
	err := r.Adapter.Run(ctx)
	r.runReturned <- err
	return err
}

func (r *originalRunObserver) WaitDrained(ctx context.Context) error {
	r.waitCalls.Add(1)
	close(r.waitEntered)
	if _, bounded := ctx.Deadline(); bounded || ctx.Err() != nil {
		return errors.New("original completion inherited a cancelled caller")
	}
	return r.Adapter.WaitDrained(ctx)
}

type originalHTTPTransport struct {
	rpc.Transport
	registered chan struct{}
	stopped    chan struct{}
}

func (tr *originalHTTPTransport) Consume(cfg rpc.ConsumerConfig) (rpc.Subscription, error) {
	sub, err := tr.Transport.Consume(cfg)
	if err != nil {
		return nil, err
	}
	close(tr.registered)
	return &originalHTTPSubscription{Subscription: sub, stopped: tr.stopped}, nil
}

type originalHTTPSubscription struct {
	rpc.Subscription
	stopped chan struct{}
	once    sync.Once
}

func (s *originalHTTPSubscription) StopAdmission() {
	s.Subscription.(interface{ StopAdmission() }).StopAdmission()
	s.once.Do(func() { close(s.stopped) })
}

func (s *originalHTTPSubscription) CloseContext(ctx context.Context) error {
	return s.Subscription.(interface{ CloseContext(context.Context) error }).CloseContext(ctx)
}

type originalBlockedReply struct {
	*httptest.ResponseRecorder
	started chan struct{}
	release <-chan struct{}
}

func (w *originalBlockedReply) Write(body []byte) (int, error) {
	close(w.started)
	<-w.release
	return w.ResponseRecorder.Write(body)
}

func waitOriginalSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("original lifecycle signal missing")
	}
}

func TestMainJoinsOriginalSDKReplyAfterRunBudgetBeforeExit(t *testing.T) {
	mux := http.NewServeMux()
	tr := &originalHTTPTransport{Transport: sdkhttp.New(&sdkhttp.Transport{Mux: mux}), registered: make(chan struct{}), stopped: make(chan struct{})}
	var calls atomic.Int32
	a := adapter.New(adapter.Config{DefaultTimeout: 20 * time.Millisecond}).Transport(tr).Register("execute", func(context.Context, rpc.Delivery) ([]byte, string, error) {
		calls.Add(1)
		return []byte("same original reply"), "text/plain", nil
	})
	owner := &originalRunObserver{Adapter: a, runReturned: make(chan error, 1), waitEntered: make(chan struct{})}
	type result struct{ run, drain error }
	done := make(chan result, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		runErr, drainErr := runAndJoinOriginalDrain(ctx, owner, cancel)
		done <- result{runErr, drainErr}
	}()
	waitOriginalSignal(t, tr.registered)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	writer := &originalBlockedReply{ResponseRecorder: httptest.NewRecorder(), started: make(chan struct{}), release: release}
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		mux.ServeHTTP(writer, httptest.NewRequest(http.MethodPost, "/rpc/execute", nil))
	}()
	waitOriginalSignal(t, writer.started)
	cancel()
	waitOriginalSignal(t, tr.stopped)
	select {
	case err := <-owner.runReturned:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Run lost its original overrun: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original bounded Run never returned")
	}
	waitOriginalSignal(t, owner.waitEntered)
	select {
	case <-done:
		t.Fatal("process path returned while the original Reply remained blocked")
	default:
	}
	refused := httptest.NewRecorder()
	mux.ServeHTTP(refused, httptest.NewRequest(http.MethodPost, "/rpc/execute", nil))
	if refused.Code != http.StatusServiceUnavailable || calls.Load() != 1 {
		t.Fatal("new work was admitted after original shutdown")
	}
	once.Do(func() { close(release) })
	waitOriginalSignal(t, requestDone)
	select {
	case got := <-done:
		if !errors.Is(got.run, context.DeadlineExceeded) || got.drain != nil || owner.waitCalls.Load() != 1 {
			t.Fatalf("original Run/Wait result changed: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("same original reply never joined")
	}
	var envelope rpc.Envelope
	if err := json.Unmarshal(writer.Body.Bytes(), &envelope); err != nil || string(envelope.Body) != "same original reply" || calls.Load() != 1 {
		t.Fatalf("original reply or invocation lost: %v", err)
	}
}

func TestMainPreservesUnqualifiedOriginalStartupError(t *testing.T) {
	owner := &originalRunObserver{Adapter: adapter.New(adapter.Config{}), runReturned: make(chan error, 1), waitEntered: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runErr, drainErr := runAndJoinOriginalDrain(ctx, owner, cancel)
	if runErr == nil || drainErr != runErr || owner.waitCalls.Load() != 1 {
		t.Fatalf("unqualified original completion became success: %v / %v", runErr, drainErr)
	}
}

func TestAuxiliaryListenerFailureStopsAdmissionAndJoinsOriginalSDKReply(t *testing.T) {
	for _, name := range []string{"health", "webhook"} {
		t.Run(name, func(t *testing.T) { auxiliaryFailureJoinsOriginalReply(t, name) })
	}
}

func auxiliaryFailureJoinsOriginalReply(t *testing.T, name string) {
	t.Helper()
	mux := http.NewServeMux()
	tr := &originalHTTPTransport{Transport: sdkhttp.New(&sdkhttp.Transport{Mux: mux}), registered: make(chan struct{}), stopped: make(chan struct{})}
	var calls atomic.Int32
	a := adapter.New(adapter.Config{DefaultTimeout: 20 * time.Millisecond}).Transport(tr).Register("execute", func(context.Context, rpc.Delivery) ([]byte, string, error) {
		calls.Add(1)
		return []byte("original reply after health failure"), "text/plain", nil
	})
	owner := &originalRunObserver{Adapter: a, runReturned: make(chan error, 1), waitEntered: make(chan struct{})}
	type result struct{ run, drain error }
	done := make(chan result, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		runErr, drainErr := runAndJoinOriginalDrain(ctx, owner, cancel)
		done <- result{runErr, drainErr}
	}()
	waitOriginalSignal(t, tr.registered)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	writer := &originalBlockedReply{ResponseRecorder: httptest.NewRecorder(), started: make(chan struct{}), release: release}
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		mux.ServeHTTP(writer, httptest.NewRequest(http.MethodPost, "/rpc/execute", nil))
	}()
	waitOriginalSignal(t, writer.started)

	// Exercise the production listener-error path after the original Reply
	// starts: binding an already-owned real socket must fail, never os.Exit.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	health := &http.Server{Addr: occupied.Addr().String()}
	defer health.Close()
	serve := health.ListenAndServe
	if name == "webhook" {
		_, port, err := net.SplitHostPort(occupied.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		webhook := ad.NewWebhookServer(&config.Config{WebhookPort: port}, nil, zap.NewNop())
		serve = func() error { return webhook.StartWithAdmissionStop(ctx, cancel) }
	}
	healthFailure := serveAuxiliaryListener(name, serve, cancel, zap.NewNop())
	select {
	case err := <-healthFailure:
		if err == nil {
			t.Fatal("actual health listener failure was lost")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("health listener did not fail on the owned socket")
	}
	waitOriginalSignal(t, tr.stopped)
	select {
	case err := <-owner.runReturned:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("original Run overrun lost: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("health failure did not stop the same Run")
	}
	waitOriginalSignal(t, owner.waitEntered)
	select {
	case <-done:
		t.Fatal("health failure escaped while the original Reply remained blocked")
	default:
	}
	refused := httptest.NewRecorder()
	mux.ServeHTTP(refused, httptest.NewRequest(http.MethodPost, "/rpc/execute", nil))
	if refused.Code != http.StatusServiceUnavailable || calls.Load() != 1 {
		t.Fatal("health failure admitted replacement work")
	}
	once.Do(func() { close(release) })
	waitOriginalSignal(t, requestDone)
	select {
	case got := <-done:
		if !errors.Is(got.run, context.DeadlineExceeded) || got.drain != nil || owner.waitCalls.Load() != 1 {
			t.Fatalf("original Run/completion changed: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("same original Reply never joined after health failure")
	}
	var envelope rpc.Envelope
	if err := json.Unmarshal(writer.Body.Bytes(), &envelope); err != nil || string(envelope.Body) != "original reply after health failure" || calls.Load() != 1 {
		t.Fatalf("original reply/invocation lost: %v", err)
	}
}
