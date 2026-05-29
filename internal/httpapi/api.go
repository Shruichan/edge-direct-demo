package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/romedrori/edge-direct-demo/internal/device"
	"github.com/romedrori/edge-direct-demo/internal/pki"
	"github.com/romedrori/edge-direct-demo/internal/store"
)

// Narrow interfaces here, not the concrete types, so handlers stay testable.

type CertIssuer interface {
	Issue(ctx context.Context, commonName string, ttl time.Duration) (pki.Cert, error)
}

type CommandPublisher interface {
	Publish(topic string, qos byte, retained bool, payload []byte) error
}

type EventPublisher interface {
	Publish(ctx context.Context, routingKey string, v any) error
}

type Server struct {
	repo      store.Repo
	issuer    CertIssuer
	commands  CommandPublisher
	events    EventPublisher
	logger    *slog.Logger
	enrollKey string
	certTTL   time.Duration
}

func New(repo store.Repo, issuer CertIssuer, commands CommandPublisher, events EventPublisher, enrollSecret string, logger *slog.Logger) *Server {
	return &Server{
		repo:      repo,
		issuer:    issuer,
		commands:  commands,
		events:    events,
		logger:    logger,
		enrollKey: enrollSecret,
		certTTL:   90 * 24 * time.Hour,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/enroll", s.handleEnroll)
	mux.HandleFunc("GET /v1/devices", s.handleListDevices)
	mux.HandleFunc("GET /v1/devices/{id}", s.handleGetDevice)
	mux.HandleFunc("POST /v1/devices/{id}/commands", s.handleSendCommand)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

type enrollRequest struct {
	TenantID       string `json:"tenant_id"`
	Serial         string `json:"serial"`
	BootstrapToken string `json:"bootstrap_token"`
}

type enrollResponse struct {
	DeviceID            string `json:"device_id"`
	Certificate         string `json:"certificate"`
	PrivateKey          string `json:"private_key"`
	IssuingCA           string `json:"issuing_ca"`
	MQTTCommandTopic    string `json:"mqtt_command_topic"`
	MQTTTelemetryTopic  string `json:"mqtt_telemetry_topic"`
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	var req enrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.Serial = strings.TrimSpace(req.Serial)
	if req.TenantID == "" || req.Serial == "" || req.BootstrapToken == "" {
		writeError(w, http.StatusBadRequest, "tenant_id, serial, bootstrap_token required")
		return
	}
	if !ValidBootstrap(s.enrollKey, req.TenantID, req.Serial, req.BootstrapToken) {
		// Don't leak which field failed — same response either way.
		writeError(w, http.StatusUnauthorized, "bootstrap rejected")
		return
	}

	id := deviceIDFor(req.TenantID, req.Serial)
	cn := id // brokers authorize on cert CN

	cert, err := s.issuer.Issue(r.Context(), cn, s.certTTL)
	if err != nil {
		s.logger.Error("vault issue failed", "err", err, "device", id)
		writeError(w, http.StatusBadGateway, "cert issuance failed")
		return
	}

	d := device.Device{
		ID:         id,
		TenantID:   req.TenantID,
		Serial:     req.Serial,
		Status:     device.StatusEnrolled,
		EnrolledAt: time.Now().UTC(),
		CertSerial: cert.SerialNumber,
	}
	if err := s.repo.CreateDevice(r.Context(), d); err != nil {
		s.logger.Error("create device failed", "err", err, "device", id)
		writeError(w, http.StatusInternalServerError, "persist failed")
		return
	}

	// Best-effort event publish; an outbox would be the next step if we cared
	// about strict delivery.
	if s.events != nil {
		_ = s.events.Publish(r.Context(), eventKey(req.TenantID, "device", "enrolled"), map[string]any{
			"device_id":   d.ID,
			"tenant_id":   d.TenantID,
			"serial":      d.Serial,
			"cert_serial": d.CertSerial,
			"enrolled_at": d.EnrolledAt,
		})
	}

	writeJSON(w, http.StatusOK, enrollResponse{
		DeviceID:           d.ID,
		Certificate:        cert.Certificate,
		PrivateKey:         cert.PrivateKey,
		IssuingCA:          cert.IssuingCA,
		MQTTCommandTopic:   device.CommandTopic(d.TenantID, d.ID),
		MQTTTelemetryTopic: device.TelemetryTopic(d.TenantID, d.ID),
	})
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	tenant := r.URL.Query().Get("tenant_id")
	devs, err := s.repo.ListDevices(r.Context(), tenant)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devs})
}

func (s *Server) handleGetDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := s.repo.GetDevice(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

type commandRequest struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

func (s *Server) handleSendCommand(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Kind == "" {
		writeError(w, http.StatusBadRequest, "kind required")
		return
	}

	d, err := s.repo.GetDevice(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	cmd := device.Command{
		ID:       uuid.NewString(),
		DeviceID: d.ID,
		Kind:     req.Kind,
		Payload:  req.Payload,
	}
	body, _ := json.Marshal(cmd)
	topic := device.CommandTopic(d.TenantID, d.ID)

	// QoS 1: AP must ack. We accept potential duplicates; the agent dedupes by
	// command id.
	if err := s.commands.Publish(topic, 1, false, body); err != nil {
		s.logger.Error("mqtt publish failed", "err", err, "device", d.ID)
		writeError(w, http.StatusBadGateway, "command dispatch failed")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"command_id": cmd.ID})
}

func deviceIDFor(tenant, serial string) string {
	// Stable, opaque, human-readable enough for logs. Re-enrolling the same
	// hardware returns the same id, which is what we want.
	return "dev_" + tenant + "_" + serial
}

func eventKey(tenant, kind, event string) string {
	return tenant + "." + kind + "." + event
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
