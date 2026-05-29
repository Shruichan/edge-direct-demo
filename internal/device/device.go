package device

import "time"

type Status string

const (
	StatusPending    Status = "pending"
	StatusEnrolled   Status = "enrolled"
	StatusActive     Status = "active"
	StatusQuarantine Status = "quarantine"
)

type Device struct {
	ID         string
	TenantID   string
	Serial     string
	Status     Status
	EnrolledAt time.Time
	LastSeen   time.Time
	CertSerial string
}

type Telemetry struct {
	DeviceID string
	TenantID string
	At       time.Time
	Uptime   int64
	Clients  int
	BSSIDs   []string
}

type Command struct {
	ID       string
	DeviceID string
	Kind     string
	Payload  []byte
}

// CommandTopic is the canonical MQTT topic an agent subscribes to. Keeping the
// shape stable here so the control plane and agent can't drift.
func CommandTopic(tenant, id string) string {
	return "tenants/" + tenant + "/devices/" + id + "/commands"
}

func TelemetryTopic(tenant, id string) string {
	return "tenants/" + tenant + "/devices/" + id + "/telemetry"
}
