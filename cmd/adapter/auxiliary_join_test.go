package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestAuxiliaryWaitBudgetStillJoinsSameOriginalServers(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	health := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		if r.Context().Err() != nil {
			t.Error("first wait budget cancelled the original request")
		}
		_, _ = w.Write([]byte("original health response"))
	})}
	healthDone := serveAuxiliaryListener("health", func() error { return health.Serve(listener) }, func() {}, zap.NewNop())
	responseDone := make(chan error, 1)
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	go func() {
		r, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			responseDone <- err
			return
		}
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err == nil && string(body) != "original health response" {
			err = errors.New("original health response changed")
		}
		responseDone <- err
	}()
	waitOriginalSignal(t, entered)
	webhookDone := make(chan error, 1)
	type result struct {
		completion auxiliaryCompletion
		waitErr    error
	}
	done := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	go func() {
		completion, waitErr := joinAuxiliaryServers(ctx, health, healthDone, webhookDone)
		done <- result{completion, waitErr}
	}()
	waitOriginalSignal(t, ctx.Done())
	select {
	case <-done:
		t.Fatal("first wait deadline escaped an original HTTP handler")
	default:
	}
	once.Do(func() { close(release) })
	select {
	case err := <-responseDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original response did not finish")
	}
	select {
	case <-done:
		t.Fatal("health completion skipped the original webhook lifetime")
	default:
	}
	webhookCause := errors.New("original webhook listener result")
	webhookDone <- webhookCause
	close(webhookDone)
	select {
	case got := <-done:
		if !errors.Is(got.waitErr, context.DeadlineExceeded) || got.completion.healthShutdown != nil ||
			!errors.Is(got.completion.healthServe, http.ErrServerClosed) || got.completion.webhookServe != webhookCause {
			t.Fatalf("original completion causes changed: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("same original auxiliary shutdowns did not join")
	}
}
