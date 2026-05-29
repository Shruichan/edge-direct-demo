package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/romedrori/edge-direct-demo/internal/pki"
	"github.com/romedrori/edge-direct-demo/internal/store"
)

type fakeIssuer struct {
	calls int
}

func (f *fakeIssuer) Issue(_ context.Context, cn string, _ time.Duration) (pki.Cert, error) {
	f.calls++
	return pki.Cert{
		Certificate:  "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n",
		PrivateKey:   "-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----\n",
		IssuingCA:    "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----\n",
		SerialNumber: "11:22:33",
	}, nil
}

type fakeCmd struct {
	topics []string
	bodies [][]byte
}

func (f *fakeCmd) Publish(topic string, _ byte, _ bool, payload []byte) error {
	f.topics = append(f.topics, topic)
	f.bodies = append(f.bodies, payload)
	return nil
}

type fakeEvents struct {
	keys []string
}

func (f *fakeEvents) Publish(_ context.Context, key string, _ any) error {
	f.keys = append(f.keys, key)
	return nil
}

func newServer(t *testing.T) (*Server, *fakeIssuer, *fakeCmd, *fakeEvents, *store.Memory) {
	t.Helper()
	repo := store.NewMemory()
	issuer := &fakeIssuer{}
	cmd := &fakeCmd{}
	ev := &fakeEvents{}
	s := New(repo, issuer, cmd, ev, "test-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	return s, issuer, cmd, ev, repo
}

func TestEnrollHappyPath(t *testing.T) {
	s, issuer, _, ev, repo := newServer(t)
	token := BootstrapToken("test-secret", "t1", "AP-1")
	body, _ := json.Marshal(map[string]string{
		"tenant_id":       "t1",
		"serial":          "AP-1",
		"bootstrap_token": token,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["device_id"] != "dev_t1_AP-1" || resp["certificate"] == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if issuer.calls != 1 {
		t.Fatalf("expected one issuer call, got %d", issuer.calls)
	}
	if _, err := repo.GetDevice(context.Background(), "dev_t1_AP-1"); err != nil {
		t.Fatalf("device not persisted: %v", err)
	}
	if len(ev.keys) != 1 || ev.keys[0] != "t1.device.enrolled" {
		t.Fatalf("expected lifecycle event, got %+v", ev.keys)
	}
}

func TestEnrollRejectsBadToken(t *testing.T) {
	s, issuer, _, _, _ := newServer(t)
	body, _ := json.Marshal(map[string]string{
		"tenant_id":       "t1",
		"serial":          "AP-1",
		"bootstrap_token": "garbage",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if issuer.calls != 0 {
		t.Fatalf("issuer must not be called when bootstrap fails")
	}
}

func TestEnrollRejectsMalformedJSON(t *testing.T) {
	s, _, _, _, _ := newServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSendCommandPublishesToCorrectTopic(t *testing.T) {
	s, _, cmd, _, repo := newServer(t)
	// Pre-enroll a device directly via the repo.
	_ = enroll(t, s, "t1", "AP-1")

	req := httptest.NewRequest(http.MethodPost, "/v1/devices/dev_t1_AP-1/commands",
		bytes.NewReader([]byte(`{"kind":"reboot","payload":{"delay_s":5}}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if len(cmd.topics) != 1 || cmd.topics[0] != "tenants/t1/devices/dev_t1_AP-1/commands" {
		t.Fatalf("wrong topic: %+v", cmd.topics)
	}
	// Make sure the device still exists after the round-trip.
	if _, err := repo.GetDevice(context.Background(), "dev_t1_AP-1"); err != nil {
		t.Fatal(err)
	}
}

func TestSendCommandNotFound(t *testing.T) {
	s, _, _, _, _ := newServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/devices/dev_nope/commands",
		bytes.NewReader([]byte(`{"kind":"reboot"}`)))
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestListDevices(t *testing.T) {
	s, _, _, _, _ := newServer(t)
	_ = enroll(t, s, "t1", "AP-1")
	_ = enroll(t, s, "t1", "AP-2")
	_ = enroll(t, s, "t2", "AP-9")

	req := httptest.NewRequest(http.MethodGet, "/v1/devices?tenant_id=t1", nil)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var out struct {
		Devices []map[string]any `json:"devices"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(out.Devices))
	}
}

func enroll(t *testing.T, s *Server, tenant, serial string) string {
	t.Helper()
	token := BootstrapToken("test-secret", tenant, serial)
	body, _ := json.Marshal(map[string]string{
		"tenant_id":       tenant,
		"serial":          serial,
		"bootstrap_token": token,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll(%s/%s) failed: %d %s", tenant, serial, rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp["device_id"]
}
