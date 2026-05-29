package device

import "testing"

func TestTopicShape(t *testing.T) {
	if got := CommandTopic("store-1234", "dev_abc"); got != "tenants/store-1234/devices/dev_abc/commands" {
		t.Fatalf("unexpected command topic: %s", got)
	}
	if got := TelemetryTopic("store-1234", "dev_abc"); got != "tenants/store-1234/devices/dev_abc/telemetry" {
		t.Fatalf("unexpected telemetry topic: %s", got)
	}
}
