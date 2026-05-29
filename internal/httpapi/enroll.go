package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// BootstrapToken is the per-device secret a freshly-flashed AP ships with. It's
// derived deterministically at manufacturing time from (tenant, serial, secret)
// so the control plane can validate without keeping a per-device row before the
// first enroll call.
func BootstrapToken(secret, tenant, serial string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(tenant))
	h.Write([]byte{0})
	h.Write([]byte(serial))
	return hex.EncodeToString(h.Sum(nil))
}

func ValidBootstrap(secret, tenant, serial, token string) bool {
	want := BootstrapToken(secret, tenant, serial)
	return hmac.Equal([]byte(want), []byte(token))
}
