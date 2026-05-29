SHELL := /bin/bash
.DEFAULT_GOAL := help

ENROLL_SECRET ?= dev-enroll-secret
TENANT        ?= store-1234
SERIAL        ?= AP-0001

export POSTGRES_DSN ?= postgres://edge:edge@localhost:5432/edge?sslmode=disable
export AMQP_URL     ?= amqp://guest:guest@localhost:5672/
export MQTT_BROKER  ?= tcp://localhost:1883
export VAULT_ADDR   ?= http://localhost:8200
export VAULT_TOKEN  ?= edge-root
export ENROLL_SECRET

help: ## list available targets
	@awk 'BEGIN{FS=":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

up: ## bring up the infra stack
	docker compose up -d

down: ## tear it down
	docker compose down -v

migrate: ## apply schema (requires psql)
	psql "$$POSTGRES_DSN" -f migrations/001_init.up.sql

vault-bootstrap: ## configure Vault PKI for ap-device role
	./scripts/bootstrap-vault.sh

build: ## compile both binaries
	go build ./...

test: ## run unit tests
	go test ./...

vet: ## static analysis
	go vet ./...

controlplane: ## run the control plane locally
	go run ./cmd/controlplane

agent: ## run a simulated AP (uses TENANT/SERIAL above)
	AGENT_TENANT_ID=$(TENANT) \
	AGENT_SERIAL=$(SERIAL) \
	AGENT_BOOTSTRAP_TOKEN=$$(go run ./tools/bootstraptoken $(ENROLL_SECRET) $(TENANT) $(SERIAL)) \
	go run ./cmd/deviceagent

token: ## print the bootstrap token a device with TENANT/SERIAL would ship with
	@go run ./tools/bootstraptoken $(ENROLL_SECRET) $(TENANT) $(SERIAL)

.PHONY: help up down migrate vault-bootstrap build test vet controlplane agent token
