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

// CommandTopic and TelemetryTopic are the one place topic strings get built.
// Both the control plane and the agent go through them, so the two sides can't
// quietly disagree on a slash or a segment and silently drop every message.
func CommandTopic(tenant, id string) string {
	return "tenants/" + tenant + "/devices/" + id + "/commands"
}

func TelemetryTopic(tenant, id string) string {
	return "tenants/" + tenant + "/devices/" + id + "/telemetry"
}
