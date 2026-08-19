package config

import (
	"errors"
	"os"
)

type Config struct {
	HTTPAddr      string
	PostgresDSN   string
	AMQPURL       string
	AMQPExchange  string
	MQTTBroker    string
	MQTTClientID  string
	MQTTUsername  string
	MQTTPassword  string
	VaultAddr     string
	VaultToken    string
	VaultPKIMount string
	VaultPKIRole  string
	EnrollSecret  string
}

func Load() (Config, error) {
	c := Config{
		HTTPAddr:      env("HTTP_ADDR", ":8080"),
		PostgresDSN:   os.Getenv("POSTGRES_DSN"),
		AMQPURL:       env("AMQP_URL", "amqp://guest:guest@localhost:5672/"),
		AMQPExchange:  env("AMQP_EXCHANGE", "edge.events"),
		MQTTBroker:    env("MQTT_BROKER", "tcp://localhost:1883"),
		MQTTClientID:  env("MQTT_CLIENT_ID", "edge-controlplane"),
		MQTTUsername:  os.Getenv("MQTT_USERNAME"),
		MQTTPassword:  os.Getenv("MQTT_PASSWORD"),
		VaultAddr:     env("VAULT_ADDR", "http://localhost:8200"),
		VaultToken:    os.Getenv("VAULT_TOKEN"),
		VaultPKIMount: env("VAULT_PKI_MOUNT", "pki-edge"),
		VaultPKIRole:  env("VAULT_PKI_ROLE", "ap-device"),
		EnrollSecret:  os.Getenv("ENROLL_SECRET"),
	}
	if c.PostgresDSN == "" {
		return c, errors.New("POSTGRES_DSN is required")
	}
	if c.EnrollSecret == "" {
		return c, errors.New("ENROLL_SECRET is required")
	}
	return c, nil
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}
