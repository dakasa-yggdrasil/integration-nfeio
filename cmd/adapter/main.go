// Command adapter is the integration-nfeio binary. Boots three listeners:
//
//  1. RPC (HTTP or AMQP, selected by YGGDRASIL_TRANSPORT) — handles
//     describe + execute on /rpc/describe + /rpc/execute (port 8081 for
//     HTTP, queue prefix yggdrasil.adapter.nfeio.* for AMQP).
//  2. Health server (port 8080) — /healthz + /readyz + /metrics.
//  3. Webhook server (port 8082) — inbound NFe.io callbacks; HMAC-verifies,
//     dedupes, normalizes status, publishes to enterprise-payments.* via
//     the publish_message capability on the rabbitmq-topology instance.
//
// The Version variable is overridden at link time by:
//
//	-ldflags="-X main.Version=v1.0.0"
package main

import (
	"context"
	"errors"
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

// Version is overridden at link time by -X main.Version=vX.Y.Z.
var Version = ad.AdapterVersion

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("config load", zap.Error(err))
	}

	templates, err := ad.LoadTemplatesDir(cfg.TemplatesDir)
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
		Version:         Version,
		DefaultTimeout:  30 * time.Second,
		Concurrency:     5,
	}).
		Register("describe", ad.DescribeHandler(logger)).
		Register("execute", ad.ExecuteHandler(logger, cli, templates, deps))

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

	// 3) Webhook server — third listener for inbound NFe.io callbacks.
	webhookSrv := ad.NewWebhookServer(cfg, cli, logger)
	coreURL := envOrDefault("YGGDRASIL_CORE_URL", "http://yggdrasil-core:9080")
	instance := envOrDefault("RABBITMQ_TOPOLOGY_INSTANCE", "rabbitmq-topology-default")
	token := os.Getenv("YGGDRASIL_RUN_TOKEN")
	dispatcher := ad.NewPublishDispatcher(coreURL, instance, token, logger)
	webhookSrv.SetPublisher(dispatcher.PublishMessage)
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
