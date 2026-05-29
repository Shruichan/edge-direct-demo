package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/romedrori/edge-direct-demo/internal/device"
)

// Memory is an in-process Repo. We use it in tests and for the agent's local
// scratch state; production runs against postgres.
type Memory struct {
	mu        sync.RWMutex
	devices   map[string]device.Device
	telemetry []device.Telemetry
}

func NewMemory() *Memory {
	return &Memory{devices: map[string]device.Device{}}
}

func (m *Memory) CreateDevice(_ context.Context, d device.Device) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices[d.ID] = d
	return nil
}

func (m *Memory) GetDevice(_ context.Context, id string) (device.Device, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.devices[id]
	if !ok {
		return device.Device{}, ErrNotFound
	}
	return d, nil
}

func (m *Memory) ListDevices(_ context.Context, tenantID string) ([]device.Device, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]device.Device, 0, len(m.devices))
	for _, d := range m.devices {
		if tenantID == "" || d.TenantID == tenantID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EnrolledAt.Before(out[j].EnrolledAt) })
	return out, nil
}

func (m *Memory) UpdateStatus(_ context.Context, id string, status device.Status, certSerial string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		return ErrNotFound
	}
	d.Status = status
	if certSerial != "" {
		d.CertSerial = certSerial
	}
	m.devices[id] = d
	return nil
}

func (m *Memory) MarkSeen(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		return ErrNotFound
	}
	d.LastSeen = at
	m.devices[id] = d
	return nil
}

func (m *Memory) InsertTelemetry(_ context.Context, t device.Telemetry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.telemetry = append(m.telemetry, t)
	return nil
}

func (m *Memory) TelemetryFor(id string) []device.Telemetry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []device.Telemetry
	for _, t := range m.telemetry {
		if t.DeviceID == id {
			out = append(out, t)
		}
	}
	return out
}
