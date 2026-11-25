package adapter

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func waitWebhookSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("original webhook lifecycle signal missing")
	}
}

func TestWebhookCancellationJoinsOriginalPublisherAndResponse(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	srv := newWebhookTestServer(t, func(queue string, payload []byte) error {
		if queue != "enterprise-payments.nfe.emitted.q" || len(payload) == 0 {
			t.Error("original normalized publication changed")
		}
		close(entered)
		<-release
		return nil
	})
	listening := make(chan string, 1)
	srv.httpSrv.BaseContext = func(l net.Listener) context.Context {
		listening <- l.Addr().String()
		return context.Background()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()
	var address string
	select {
	case address = <-listening:
	case <-time.After(5 * time.Second):
		t.Fatal("original webhook listener did not start")
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	url := "http://127.0.0.1:" + port + "/webhook/nfeio"
	body := `{"id":"original-shutdown","event":"nfse.issued","data":{"id":"i","status":"Issued"}}`
	request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Hub-Signature", computeHMAC(webhookTestSecret, body))
	type response struct {
		status int
		body   []byte
		err    error
	}
	responseDone := make(chan response, 1)
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	go func() {
		r, err := client.Do(request)
		if err != nil {
			responseDone <- response{err: err}
			return
		}
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		responseDone <- response{status: r.StatusCode, body: body, err: err}
	}()
	waitWebhookSignal(t, entered)
	cancel()
	// Serve returning proves admission closed, while Shutdown still owns the
	// original publisher/response. Wait for the real socket to refuse a dial.
	deadline := time.Now().Add(5 * time.Second)
	for {
		connection, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 50*time.Millisecond)
		if err != nil {
			break
		}
		connection.Close()
		if time.Now().After(deadline) {
			t.Fatal("webhook admission remained open after cancellation")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("Start returned while original publisher was active: %v", err)
	default:
	}
	select {
	case got := <-responseDone:
		t.Fatalf("original response was interrupted before publisher return: %+v", got)
	default:
	}
	once.Do(func() { close(release) })
	select {
	case got := <-responseDone:
		if got.err != nil || got.status != http.StatusAccepted || len(got.body) != 0 {
			t.Fatalf("original HTTP publication/response changed: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original webhook response did not join")
	}
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("original listener result changed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original webhook shutdown did not join")
	}
}

func TestWebhookShutdownBeforeStartCannotBindLater(t *testing.T) {
	srv := newWebhookTestServer(t, nil)
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("shutdown server was revived: %v", err)
	}
}

func TestWebhookBindFailureStopsSiblingAdmissionAndJoinsWatcher(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	srv := newWebhookTestServer(t, nil)
	srv.httpSrv.Addr = occupied.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.StartWithAdmissionStop(ctx, cancel); err == nil || errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("original bind failure lost: %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("webhook bind failure left sibling admission active")
	}
}
