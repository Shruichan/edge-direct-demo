package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/romedrori/edge-direct-demo/internal/device"
	"github.com/romedrori/edge-direct-demo/internal/mqttbus"
)

// Simulated IoT access point. Enrolls against the control plane, persists the
// returned cert + key on disk, then publishes telemetry on a loop.

type agentConfig struct {
	TenantID       string
	Serial         string
	BootstrapToken string
	ControlPlane   string
	MQTTBroker     string
	CertDir        string
	Period         time.Duration
}

func loadAgentConfig() (agentConfig, error) {
	c := agentConfig{
		TenantID:       os.Getenv("AGENT_TENANT_ID"),
		Serial:         os.Getenv("AGENT_SERIAL"),
		BootstrapToken: os.Getenv("AGENT_BOOTSTRAP_TOKEN"),
		ControlPlane:   envOr("AGENT_CONTROL_PLANE", "http://localhost:8080"),
		MQTTBroker:     envOr("AGENT_MQTT_BROKER", "tcp://localhost:1883"),
		CertDir:        envOr("AGENT_CERT_DIR", "./certs"),
		Period:         15 * time.Second,
	}
	if c.TenantID == "" || c.Serial == "" || c.BootstrapToken == "" {
		return c, errors.New("AGENT_TENANT_ID, AGENT_SERIAL, AGENT_BOOTSTRAP_TOKEN are required")
	}
	return c, nil
}

type enrollment struct {
	DeviceID           string
	CommandTopic       string
	TelemetryTopic     string
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("agent exited", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := loadAgentConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	enr, err := ensureEnrolled(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("enroll: %w", err)
	}
	logger.Info("enrolled", "device_id", enr.DeviceID)

	mq, err := mqttbus.Connect(ctx, mqttbus.Options{
		Broker:   cfg.MQTTBroker,
		ClientID: enr.DeviceID,
		Logger:   logger,
	})
	if err != nil {
		return err
	}
	defer mq.Disconnect()

	if err := mq.Subscribe(enr.CommandTopic, 1, func(_ string, payload []byte) {
		var cmd device.Command
		if err := json.Unmarshal(payload, &cmd); err != nil {
			logger.Warn("bad command payload", "err", err)
			return
		}
		logger.Info("received command", "id", cmd.ID, "kind", cmd.Kind)
		// Real device would dispatch on Kind — restart, refresh config, etc.
	}); err != nil {
		return err
	}

	tick := time.NewTicker(cfg.Period)
	defer tick.Stop()

	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			t := device.Telemetry{
				DeviceID: enr.DeviceID,
				TenantID: cfg.TenantID,
				At:       time.Now().UTC(),
				Uptime:   int64(time.Since(start).Seconds()),
				Clients:  rand.Intn(40),
				BSSIDs:   []string{"edge-corp", "edge-guest"},
			}
			body, _ := json.Marshal(t)
			if err := mq.Publish(enr.TelemetryTopic, 1, false, body); err != nil {
				logger.Warn("publish failed", "err", err)
			}
		}
	}
}

// ensureEnrolled checks the cert dir for prior enrollment. If anything's
// missing it calls /v1/enroll and writes the response to disk. The marker file
// "device.json" records the topic strings so we don't have to re-derive them.
func ensureEnrolled(ctx context.Context, cfg agentConfig, logger *slog.Logger) (enrollment, error) {
	if err := os.MkdirAll(cfg.CertDir, 0o755); err != nil {
		return enrollment{}, err
	}
	markerPath := filepath.Join(cfg.CertDir, "device.json")
	if data, err := os.ReadFile(markerPath); err == nil {
		var enr enrollment
		if err := json.Unmarshal(data, &enr); err == nil && enr.DeviceID != "" {
			return enr, nil
		}
	}

	type enrollReq struct {
		TenantID       string `json:"tenant_id"`
		Serial         string `json:"serial"`
		BootstrapToken string `json:"bootstrap_token"`
	}
	type enrollResp struct {
		DeviceID           string `json:"device_id"`
		Certificate        string `json:"certificate"`
		PrivateKey         string `json:"private_key"`
		IssuingCA          string `json:"issuing_ca"`
		MQTTCommandTopic   string `json:"mqtt_command_topic"`
		MQTTTelemetryTopic string `json:"mqtt_telemetry_topic"`
	}

	body, _ := json.Marshal(enrollReq{cfg.TenantID, cfg.Serial, cfg.BootstrapToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.ControlPlane+"/v1/enroll", bytes.NewReader(body))
	if err != nil {
		return enrollment{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return enrollment{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return enrollment{}, fmt.Errorf("enroll rejected: %s: %s", resp.Status, string(b))
	}
	var er enrollResp
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return enrollment{}, err
	}

	// Persist cert + key with restrictive perms.
	if err := os.WriteFile(filepath.Join(cfg.CertDir, "cert.pem"), []byte(er.Certificate), 0o644); err != nil {
		return enrollment{}, err
	}
	if err := os.WriteFile(filepath.Join(cfg.CertDir, "key.pem"), []byte(er.PrivateKey), 0o600); err != nil {
		return enrollment{}, err
	}
	if er.IssuingCA != "" {
		_ = os.WriteFile(filepath.Join(cfg.CertDir, "ca.pem"), []byte(er.IssuingCA), 0o644)
	}

	enr := enrollment{
		DeviceID:       er.DeviceID,
		CommandTopic:   er.MQTTCommandTopic,
		TelemetryTopic: er.MQTTTelemetryTopic,
	}
	marker, _ := json.Marshal(enr)
	_ = os.WriteFile(markerPath, marker, 0o644)

	logger.Info("certificate written", "dir", cfg.CertDir)
	return enr, nil
}

func envOr(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}
