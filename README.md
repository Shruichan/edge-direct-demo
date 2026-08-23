# edge-direct-demo

A miniature management plane for a fleet of IoT access points, built the way a
production edge platform is built: Go services, Postgres, RabbitMQ, MQTT, and
HashiCorp Vault PKI.

The point isn't to be feature-complete. It's to exercise the integration points
end to end, so the design tradeoffs (cert issuance flow, telemetry fan-out,
command dispatch, schema choices) are concrete instead of hand-wavy.

## What's in the box

```
cmd/
  controlplane/      HTTP API + MQTT subscriber + AMQP publisher
  deviceagent/       Simulated access point; enrolls and publishes telemetry
internal/
  config/            env-driven configuration
  device/            domain types + topic conventions
  store/             Repo interface, Postgres impl (pgx), in-memory impl
  pki/               Vault PKI client (mTLS cert issuance + revocation)
  mqttbus/           Paho wrapper (publish, subscribe, reconnect)
  eventbus/          RabbitMQ topic-exchange publisher
  httpapi/           enroll / list / get / send command endpoints
migrations/          Postgres schema
scripts/             mosquitto.conf, vault bootstrap
tools/bootstraptoken Tiny utility for generating per-device tokens
```

## Architecture

```
  ┌───────────┐   POST /v1/enroll      ┌────────────────┐    issue cert    ┌───────┐
  │ device    │ ─────────────────────► │  controlplane  │ ───────────────► │ Vault │
  │  agent    │ ◄───────────── cert ── │   (HTTP API)   │ ◄────── cert ─── │  PKI  │
  └────┬──────┘                        └───┬─────────┬──┘                  └───────┘
       │ MQTT (telemetry)                  │         │ AMQP (events)
       ▼                                   ▼         ▼
  ┌──────────┐    telemetry topic     ┌──────────┐  ┌───────────┐
  │ mosquitto│ ────────────────────► │ controlplane │  │ rabbitmq │
  └──────────┘  commands ◄──────────  └─────┬────┘   └─────┬────┘
                                            │              │
                                       writes ▼              ▼ binds
                                       ┌──────────┐    other internal
                                       │ postgres │    consumers
                                       └──────────┘
```

### Enrollment flow

1. At manufacturing, a device is flashed with `(tenant_id, serial)` and a
   `bootstrap_token = HMAC-SHA256(ENROLL_SECRET, tenant_id || 0 || serial)`.
2. On first boot, the agent POSTs `/v1/enroll` with those three fields.
3. The control plane validates the HMAC, asks Vault to issue a leaf cert with
   the device id as common name, and persists the device + cert serial.
4. The agent stores the cert and key under `./certs/` and re-uses them on
   subsequent boots. Re-enrolling the same hardware is idempotent.

### Telemetry

Devices publish JSON to `tenants/<t>/devices/<id>/telemetry` over MQTT (QoS 1,
non-retained). The control plane subscribes on the `tenants/+/devices/+/telemetry`
wildcard, inserts a row into `telemetry`, advances `devices.last_seen`, and
publishes the same payload to the `edge.events` topic exchange on RabbitMQ
under routing key `<tenant>.device.telemetry`. Downstream consumers (alerting,
analytics) bind their own queues against that exchange.

### Commands

`POST /v1/devices/{id}/commands` publishes a JSON command to
`tenants/<t>/devices/<id>/commands` on MQTT (QoS 1, clean_session=false on the
broker side, so commands queue while a device is offline). The agent dedupes
by command id.

## Running it locally

```bash
make up               # postgres, rabbitmq, mosquitto, vault (dev mode)
make migrate          # apply schema
make vault-bootstrap  # configure PKI engine + ap-device role
make controlplane     # in one terminal

# in another:
make agent TENANT=store-1234 SERIAL=AP-0001
```

Then poke the API:

```bash
curl localhost:8080/v1/devices | jq
curl -X POST localhost:8080/v1/devices/dev_store-1234_AP-0001/commands \
  -d '{"kind":"reboot"}' -H 'content-type: application/json'
```

The agent log will show the command being received over MQTT.

## Mapping to the role

| Requirement                              | Where it shows up                                   |
|------------------------------------------|-----------------------------------------------------|
| Production Go services in containers     | `cmd/{controlplane,deviceagent}`, `docker-compose`  |
| Postgres at scale                        | `internal/store/postgres.go`, schema in `migrations`|
| Message processing (AMQP)                | `internal/eventbus/amqp.go`                         |
| MQTT broker on the device path           | `internal/mqttbus`, telemetry + command topics      |
| PKI + Vault-managed CA                   | `internal/pki`, `scripts/bootstrap-vault.sh`        |
| Build from requirements to working code  | this repo                                           |

## What I'd add next

- **Outbox table** for enroll/lifecycle events so they survive a broker
  outage (the current publish is best-effort).
- **mTLS** on the MQTT listener (Mosquitto config exists, the cert flow is
  already there; just wasn't wired into the dev compose).
- **CRL / OCSP** polling so revocations propagate to the broker's auth.
- **Migration plan to WCNP** notes — separating the AKS-specific bits (none
  yet, but `docker-compose` would become a Helm chart and the operator pattern
  for Vault auth would shift to Kubernetes SA tokens).
