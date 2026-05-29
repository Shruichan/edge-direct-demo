package store

import (
	"context"
	"errors"
	"time"

	"github.com/romedrori/edge-direct-demo/internal/device"
)

var ErrNotFound = errors.New("device: not found")

type Repo interface {
	CreateDevice(ctx context.Context, d device.Device) error
	GetDevice(ctx context.Context, id string) (device.Device, error)
	ListDevices(ctx context.Context, tenantID string) ([]device.Device, error)
	UpdateStatus(ctx context.Context, id string, status device.Status, certSerial string) error
	MarkSeen(ctx context.Context, id string, at time.Time) error
	InsertTelemetry(ctx context.Context, t device.Telemetry) error
}
