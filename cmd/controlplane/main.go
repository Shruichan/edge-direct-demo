package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/romedrori/edge-direct-demo/internal/config"
	"github.com/romedrori/edge-direct-demo/internal/device"
	"github.com/romedrori/edge-direct-demo/internal/eventbus"
	"github.com/romedrori/edge-direct-demo/internal/httpapi"
	"github.com/romedrori/edge-direct-demo/internal/mqttbus"
	"github.com/romedrori/edge-direct-demo/internal/pki"
	"github.com/romedrori/edge-direct-demo/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(logger); err != nil {
		logger.Error("controlplane exited with error", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	repo, err := store.NewPostgres(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer repo.Close()

	issuer, err := pki.New(cfg.VaultAddr, cfg.VaultToken, cfg.VaultPKIMount, cfg.VaultPKIRole)
	if err != nil {
		return err
	}

	events, err := eventbus.Dial(ctx, cfg.AMQPURL, cfg.AMQPExchange, logger)
	if err != nil {
		return err
	}
	defer events.Close()

	mq, err := mqttbus.Connect(ctx, mqttbus.Options{
		Broker:   cfg.MQTTBroker,
		ClientID: cfg.MQTTClientID,
		Username: cfg.MQTTUsername,
		Password: cfg.MQTTPassword,
		Logger:   logger,
	})
	if err != nil {
		return err
	}
	defer mq.Disconnect()

	// Telemetry ingest: subscribe to all tenants/devices, persist, fan out.
	if err := mq.Subscribe("tenants/+/devices/+/telemetry", 1, func(topic string, payload []byte) {
		ingestTelemetry(context.Background(), logger, repo, events, topic, payload)
	}); err != nil {
		return err
	}

	api := httpapi.New(repo, issuer, mq, events, cfg.EnrollSecret, logger)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func ingestTelemetry(ctx context.Context, logger *slog.Logger, repo store.Repo, events *eventbus.Publisher, topic string, payload []byte) {
	var t device.Telemetry
	if err := json.Unmarshal(payload, &t); err != nil {
		logger.Warn("bad telemetry payload", "topic", topic, "err", err)
		return
	}
	if t.At.IsZero() {
		t.At = time.Now().UTC()
	}
	if err := repo.InsertTelemetry(ctx, t); err != nil {
		logger.Error("telemetry insert failed", "err", err, "device", t.DeviceID)
		return
	}
	if err := repo.MarkSeen(ctx, t.DeviceID, t.At); err != nil {
		logger.Warn("mark seen failed", "err", err, "device", t.DeviceID)
	}
	_ = events.Publish(ctx, eventbus.RoutingKey(t.TenantID, "device", "telemetry"), t)
}
