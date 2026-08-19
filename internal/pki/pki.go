package pki

import (
	"context"
	"errors"
	"fmt"
	"time"

	vault "github.com/hashicorp/vault/api"
)

type Cert struct {
	Certificate  string
	PrivateKey   string
	IssuingCA    string
	SerialNumber string
	Expires      time.Time
}

type Issuer struct {
	client *vault.Client
	mount  string // e.g. "pki-edge"
	role   string // e.g. "ap-device"
}

func New(addr, token, mount, role string) (*Issuer, error) {
	cfg := vault.DefaultConfig()
	cfg.Address = addr
	c, err := vault.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	c.SetToken(token)
	return &Issuer{client: c, mount: mount, role: role}, nil
}

// Issue asks Vault to mint a leaf cert for the device. Common name carries the
// device id so brokers can authorize on cert subject without an extra lookup.
func (i *Issuer) Issue(ctx context.Context, commonName string, ttl time.Duration) (Cert, error) {
	path := fmt.Sprintf("%s/issue/%s", i.mount, i.role)
	secret, err := i.client.Logical().WriteWithContext(ctx, path, map[string]any{
		"common_name": commonName,
		"ttl":         ttl.String(),
		"format":      "pem",
	})
	if err != nil {
		return Cert{}, err
	}
	if secret == nil || secret.Data == nil {
		return Cert{}, errors.New("pki: empty response from vault")
	}
	c := Cert{}
	c.Certificate, _ = secret.Data["certificate"].(string)
	c.PrivateKey, _ = secret.Data["private_key"].(string)
	c.IssuingCA, _ = secret.Data["issuing_ca"].(string)
	c.SerialNumber, _ = secret.Data["serial_number"].(string)

	if exp, ok := secret.Data["expiration"].(float64); ok {
		c.Expires = time.Unix(int64(exp), 0)
	}
	if c.Certificate == "" || c.PrivateKey == "" {
		return Cert{}, errors.New("pki: malformed response from vault")
	}
	return c, nil
}

// Revoke pulls a device's cert when it's decommissioned or quarantined. Nothing
// calls it yet — the quarantine flow is still a TODO — but the plumbing's here.
func (i *Issuer) Revoke(ctx context.Context, serial string) error {
	path := fmt.Sprintf("%s/revoke", i.mount)
	_, err := i.client.Logical().WriteWithContext(ctx, path, map[string]any{
		"serial_number": serial,
	})
	return err
}
