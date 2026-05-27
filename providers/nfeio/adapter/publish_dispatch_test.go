package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestPublishDispatch_PostsToYggdrasilCore(t *testing.T) {
	var receivedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewPublishDispatcher(srv.URL, "instance-rabbit-1", "fakeToken", zap.NewNop())
	err := d.PublishMessage("enterprise-payments.nfe.emitted.q", []byte(`{"hello":"world"}`))
	if err != nil {
		t.Fatalf("PublishMessage err = %v", err)
	}
	if receivedPath == "" {
		t.Fatal("dispatcher did not POST anywhere")
	}
}
