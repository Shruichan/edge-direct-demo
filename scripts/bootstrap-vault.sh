#!/usr/bin/env bash
# Configures a dev Vault with a PKI engine and an "ap-device" role suitable
# for issuing short-lived leaf certs to access points.
set -euo pipefail

: "${VAULT_ADDR:=http://localhost:8200}"
: "${VAULT_TOKEN:=edge-root}"
: "${PKI_MOUNT:=pki-edge}"
: "${PKI_ROLE:=ap-device}"
: "${COMMON_NAME:=edge.internal}"

export VAULT_ADDR VAULT_TOKEN

vault secrets enable -path="${PKI_MOUNT}" pki 2>/dev/null || true
vault secrets tune -max-lease-ttl=87600h "${PKI_MOUNT}"

# Root CA. In production this would be an intermediate signed by an offline root.
vault write -field=certificate "${PKI_MOUNT}/root/generate/internal" \
    common_name="${COMMON_NAME}" ttl=87600h > /tmp/edge-ca.crt

vault write "${PKI_MOUNT}/config/urls" \
    issuing_certificates="${VAULT_ADDR}/v1/${PKI_MOUNT}/ca" \
    crl_distribution_points="${VAULT_ADDR}/v1/${PKI_MOUNT}/crl"

vault write "${PKI_MOUNT}/roles/${PKI_ROLE}" \
    allowed_domains="${COMMON_NAME},edge.local" \
    allow_subdomains=true \
    allow_bare_domains=true \
    allow_any_name=true \
    enforce_hostnames=false \
    client_flag=true \
    server_flag=false \
    max_ttl=2160h

echo "Vault PKI ready at ${PKI_MOUNT}/issue/${PKI_ROLE}"
