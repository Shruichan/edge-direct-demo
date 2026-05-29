package store

import (
	"context"
	"testing"
	"time"

	"github.com/romedrori/edge-direct-demo/internal/device"
)

func TestMemoryCRUD(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()

	d := device.Device{
		ID:         "dev_t_1",
		TenantID:   "t",
		Serial:     "1",
		Status:     device.StatusPending,
		EnrolledAt: time.Now().UTC(),
	}
	if err := m.CreateDevice(ctx, d); err != nil {
		t.Fatal(err)
	}

	got, err := m.GetDevice(ctx, d.ID)
	if err != nil || got.ID != d.ID {
		t.Fatalf("GetDevice: %v, %+v", err, got)
	}

	if err := m.UpdateStatus(ctx, d.ID, device.StatusEnrolled, "serial-xyz"); err != nil {
		t.Fatal(err)
	}
	got, _ = m.GetDevice(ctx, d.ID)
	if got.Status != device.StatusEnrolled || got.CertSerial != "serial-xyz" {
		t.Fatalf("status/serial not updated: %+v", got)
	}

	now := time.Now().UTC()
	if err := m.MarkSeen(ctx, d.ID, now); err != nil {
		t.Fatal(err)
	}
	got, _ = m.GetDevice(ctx, d.ID)
	if !got.LastSeen.Equal(now) {
		t.Fatalf("last_seen not advanced")
	}

	if err := m.InsertTelemetry(ctx, device.Telemetry{DeviceID: d.ID, At: now, Clients: 3}); err != nil {
		t.Fatal(err)
	}
	if got := m.TelemetryFor(d.ID); len(got) != 1 || got[0].Clients != 3 {
		t.Fatalf("telemetry not stored: %+v", got)
	}
}

func TestMemoryListByTenant(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	t0 := time.Now().UTC()
	_ = m.CreateDevice(ctx, device.Device{ID: "a", TenantID: "t1", EnrolledAt: t0})
	_ = m.CreateDevice(ctx, device.Device{ID: "b", TenantID: "t2", EnrolledAt: t0.Add(time.Second)})
	_ = m.CreateDevice(ctx, device.Device{ID: "c", TenantID: "t1", EnrolledAt: t0.Add(2 * time.Second)})

	got, err := m.ListDevices(ctx, "t1")
	if err != nil || len(got) != 2 {
		t.Fatalf("expected 2 devices for t1, got %d (%v)", len(got), err)
	}
	if got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("list not ordered by enrolled_at: %+v", got)
	}
}

func TestMemoryNotFound(t *testing.T) {
	m := NewMemory()
	_, err := m.GetDevice(context.Background(), "nope")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
