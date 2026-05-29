package mqttbus

import (
	"context"
	"errors"
	"log/slog"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Client struct {
	c       mqtt.Client
	logger  *slog.Logger
	timeout time.Duration
}

type Options struct {
	Broker   string
	ClientID string
	Username string
	Password string
	Logger   *slog.Logger
}

func Connect(ctx context.Context, opts Options) (*Client, error) {
	mo := mqtt.NewClientOptions().
		AddBroker(opts.Broker).
		SetClientID(opts.ClientID).
		SetAutoReconnect(true).
		SetCleanSession(false). // queue commands while devices are offline
		SetOrderMatters(false).
		SetKeepAlive(30 * time.Second)
	if opts.Username != "" {
		mo.SetUsername(opts.Username).SetPassword(opts.Password)
	}

	c := mqtt.NewClient(mo)
	tok := c.Connect()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-tokenDone(tok):
	}
	if err := tok.Error(); err != nil {
		return nil, err
	}
	return &Client{c: c, logger: opts.Logger, timeout: 5 * time.Second}, nil
}

func (c *Client) Publish(topic string, qos byte, retained bool, payload []byte) error {
	t := c.c.Publish(topic, qos, retained, payload)
	if !t.WaitTimeout(c.timeout) {
		return errors.New("mqtt: publish timed out")
	}
	return t.Error()
}

func (c *Client) Subscribe(topic string, qos byte, handler func(topic string, payload []byte)) error {
	t := c.c.Subscribe(topic, qos, func(_ mqtt.Client, msg mqtt.Message) {
		// Paho dispatches in a goroutine pool; handlers must be quick or hand off.
		handler(msg.Topic(), msg.Payload())
	})
	if !t.WaitTimeout(c.timeout) {
		return errors.New("mqtt: subscribe timed out")
	}
	return t.Error()
}

func (c *Client) Disconnect() {
	c.c.Disconnect(250)
}

func tokenDone(t mqtt.Token) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		t.Wait()
		close(ch)
	}()
	return ch
}
