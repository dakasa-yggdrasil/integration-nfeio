// Command adapter is the integration-nfeio binary. Boots three listeners:
//
//  1. RPC (HTTP or AMQP, selected by YGGDRASIL_TRANSPORT) — handles
//     describe + execute on /rpc/describe + /rpc/execute (port 8081 for
//     HTTP, queue prefix yggdrasil.adapter.nfeio.* for AMQP).
//  2. Health server (port 8080) — /healthz + /readyz + /metrics.
//  3. Webhook server (port 8082): legacy normalized callbacks. HMAC-verifies,
//     dedupes, normalizes status, and hands the event to a publisher. The
//     publish dispatcher is disabled unless YGGDRASIL_CORE_BASE_URL and
//     YGGDRASIL_WORKFLOW_RUN_TOKEN are both set; without it the event is
//     logged and dropped.
//
// The adapter package's AdapterVersion is overridden at link time by:
//
//	-ldflags="-X github.com/dakasa-yggdrasil/integration-nfeio/providers/nfeio/adapter.AdapterVersion=1.0.0"
package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	sdkadapter "github.com/dakasa-yggdrasil/yggdrasil-sdk-go/adapter"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	ad "github.com/dakasa-yggdrasil/integration-nfeio/providers/nfeio/adapter"
	"github.com/dakasa-yggdrasil/integration-nfeio/providers/nfeio/config"
)

// Templates copied here at build time from manifest/templates/ (Go embed
// cannot escape with '..'; cp -r happens in Dockerfile and CI).
//
//go:embed templates/*.yaml
var embeddedTemplates embed.FS

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		_, _ = os.Stdout.WriteString(ad.AdapterVersion + "\n")
		return
	}

	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("config load", zap.Error(err))
	}

	// Prefer filesystem dir when TEMPLATES_DIR is set + readable (dev/tests);
	// otherwise fall back to embedded templates baked into the binary.
	var templates map[string]*ad.MunicipioTemplate
	if _, statErr := os.Stat(cfg.TemplatesDir); statErr == nil {
		templates, err = ad.LoadTemplatesDir(cfg.TemplatesDir)
	} else {
		var sub fs.FS
		sub, err = fs.Sub(embeddedTemplates, "templates")
		if err == nil {
			templates, err = ad.LoadTemplatesFS(sub, ".")
		}
	}
	if err != nil {
		logger.Fatal("template load", zap.Error(err))
	}
	logger.Info("templates loaded", zap.Int("count", len(templates)))
	for code := range templates {
		ad.MetricTemplateLoad().WithLabelValues(code).Set(1)
	}

	cli, err := ad.NewClient(cfg, logger)
	if err != nil {
		logger.Fatal("nfeio client", zap.Error(err))
	}

	municipalitiesCache := ad.NewMunicipalitiesCache(1 * time.Hour)
	deps := &ad.ExecuteDeps{MunicipalitiesCache: municipalitiesCache}

	a := sdkadapter.New(sdkadapter.Config{
		Provider:        ad.Provider,
		IntegrationType: ad.IntegrationType,
		Version:         ad.AdapterVersion,
		DefaultTimeout:  30 * time.Second,
		Concurrency:     5,
	})

	// Install the SDK reconcile dispatch table before the "execute" handler.
	// ExecuteHandler
	// routes inbound envelopes through reconcile.Dispatch first
	// (activating §6.5 mutation event auto-emission via the
	// WireReconcilersWithInstance-installed dispatch path), and falls
	// back to executeRoute for ops outside the
	// ensure_/observe_/destroy_ triples — retrieve_pdf, retrieve_xml,
	// manage_template, bulk_issue, calculate_iss and observe_municipalities
	// (cache-backed, not a Reconciler). The static instanceID stays empty
	// on purpose: ExecuteHandler lifts Core's integration.instance.name
	// into each envelope, and the SDK prefers that per-call name. One
	// Deployment serves several integration instances, so a static label
	// would mislabel events. An envelope without an instance name yields
	// an event with an empty instance_id, which Core refuses (fail closed).
	ad.WireReconcilersWithInstance(a, cli, templates, "")

	a.Register("describe", ad.DescribeHandler(logger)).
		Register("execute", ad.ExecuteHandler(logger, a, cli, templates, deps))

	// 1) RPC transport (HTTP or AMQP) — selected at startup.
	switch transport := strings.ToLower(strings.TrimSpace(os.Getenv("YGGDRASIL_TRANSPORT"))); transport {
	case "", "http", "http_json":
		addr := ":" + envOrDefault("RPC_PORT", "8081")
		a.ListenHTTP(addr)
		logger.Info("nfeio adapter starting on HTTP", zap.String("addr", addr))
	case "amqp", "rabbitmq":
		brokerURL := strings.TrimSpace(os.Getenv("BROKER_URL"))
		if brokerURL == "" {
			logger.Fatal("YGGDRASIL_TRANSPORT=amqp but BROKER_URL is empty")
		}
		a.ListenAMQP(brokerURL)
		logger.Info("nfeio adapter starting on AMQP")
	default:
		logger.Fatal("unsupported YGGDRASIL_TRANSPORT", zap.String("value", transport))
	}

	// 2) Health server — separate port so probes survive RPC degradation.
	healthSrv := newHealthServer(cfg.HealthPort)
	go func() {
		if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("health server", zap.Error(err))
		}
	}()

	// 3) Webhook server: third listener, for legacy normalized callbacks.
	// The publish dispatcher has its own URL and bearer pair and never reads
	// YGGDRASIL_CORE_URL or YGGDRASIL_RUN_TOKEN, which belong to the
	// mutation event emitter (YGGDRASIL_RUN_TOKEN is this adapter's own
	// event publisher bearer). It targets /api/v1/capabilities/invoke, a
	// route Core does not have, so it stays disabled unless both of its env
	// vars are set.
	webhookSrv := ad.NewWebhookServer(cfg, cli, logger)
	instance := envOrDefault("RABBITMQ_TOPOLOGY_INSTANCE", "rabbitmq-topology-default")
	if dispatcher := ad.PublishDispatcherFromEnv(os.Getenv, instance, logger); dispatcher != nil {
		webhookSrv.SetPublisher(dispatcher.PublishMessage)
	} else {
		logger.Warn("legacy webhook publish dispatcher disabled; webhook events are logged and dropped",
			zap.String("requires", ad.EnvPublishCoreURL+" and "+ad.EnvPublishToken))
	}
	webhookCtx, cancelWebhook := context.WithCancel(context.Background())
	defer cancelWebhook()
	go func() {
		if err := webhookSrv.Start(webhookCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("webhook server", zap.Error(err))
		}
	}()

	ctx := sdkadapter.WithSignalHandler(context.Background())
	if err := a.Run(ctx); err != nil {
		logger.Fatal("adapter run", zap.Error(err))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = healthSrv.Shutdown(shutdownCtx)
	_ = webhookSrv.Shutdown(shutdownCtx)
}

// newHealthServer mounts /healthz (always 200), /readyz (always 200; the
// AMQP transport handles reconnect internally so adapter readiness is
// effectively "templates loaded and main loop running") and /metrics.
func newHealthServer(port string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.Handle("/metrics", promhttp.Handler())
	return &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func envOrDefault(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
