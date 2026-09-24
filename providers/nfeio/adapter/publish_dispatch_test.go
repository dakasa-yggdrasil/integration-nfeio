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

// fakeEnv returns a getenv that only sees the given values.
func fakeEnv(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestPublishDispatcherFromEnv_SendsWorkflowRunTokenNotEventBearer(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := PublishDispatcherFromEnv(fakeEnv(map[string]string{
		EnvPublishCoreURL:     srv.URL + "/",
		EnvPublishToken:       "workflow-run-bearer",
		"YGGDRASIL_CORE_URL":  "http://event-emitter.invalid",
		"YGGDRASIL_RUN_TOKEN": "event-publisher-bearer",
	}), "instance-rabbit-1", zap.NewNop())
	if d == nil {
		t.Fatal("dispatcher is nil with both publish env vars set")
	}
	if err := d.PublishMessage("enterprise-payments.nfe.emitted.q", []byte(`{"hello":"world"}`)); err != nil {
		t.Fatalf("PublishMessage err = %v", err)
	}
	if gotAuth != "Bearer workflow-run-bearer" {
		t.Fatalf("Authorization = %q; want the %s bearer", gotAuth, EnvPublishToken)
	}
	if gotPath != "/api/v1/capabilities/invoke" {
		t.Fatalf("path = %q; want /api/v1/capabilities/invoke on the %s host", gotPath, EnvPublishCoreURL)
	}
}

func TestPublishDispatcherFromEnv_DisabledWithoutItsOwnEnvPair(t *testing.T) {
	cases := map[string]map[string]string{
		"nothing set": {},
		"no url": {
			EnvPublishToken: "workflow-run-bearer",
		},
		"no token": {
			EnvPublishCoreURL: "http://yggdrasil.invalid:9080",
		},
		"only the event emitter pair": {
			"YGGDRASIL_CORE_URL":  "http://yggdrasil.invalid:9080",
			"YGGDRASIL_RUN_TOKEN": "event-publisher-bearer",
		},
		"publish url with the event bearer": {
			EnvPublishCoreURL:     "http://yggdrasil.invalid:9080",
			"YGGDRASIL_RUN_TOKEN": "event-publisher-bearer",
		},
		"blank values": {
			EnvPublishCoreURL: "   ",
			EnvPublishToken:   "   ",
		},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if d := PublishDispatcherFromEnv(fakeEnv(env), "instance-rabbit-1", zap.NewNop()); d != nil {
				t.Fatal("dispatcher is enabled; want nil (disabled)")
			}
		})
	}
}
