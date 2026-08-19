package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Publisher fans out lifecycle + telemetry events to a topic exchange. Other
// internal services (analytics, alerting) bind their own queues against this.
type Publisher struct {
	conn     *amqp.Connection
	ch       *amqp.Channel
	exchange string
	logger   *slog.Logger
	mu       sync.Mutex
}

func Dial(ctx context.Context, url, exchange string, logger *slog.Logger) (*Publisher, error) {
	// amqp.Dial blocks and ignores context, so we run it in a goroutine and
	// race it against ctx ourselves.
	type dialResult struct {
		c   *amqp.Connection
		err error
	}
	resCh := make(chan dialResult, 1)
	go func() {
		c, err := amqp.Dial(url)
		resCh <- dialResult{c, err}
	}()

	var conn *amqp.Connection
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-resCh:
		if r.err != nil {
			return nil, r.err
		}
		conn = r.c
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		conn.Close()
		return nil, err
	}
	return &Publisher{conn: conn, ch: ch, exchange: exchange, logger: logger}, nil
}

// Publish serializes v as JSON. Routing key shape: <tenant>.<kind>.<event>
// e.g. "store-1234.device.enrolled" — gives consumers flexible bind patterns.
func (p *Publisher) Publish(ctx context.Context, routingKey string, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	pubCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	return p.ch.PublishWithContext(pubCtx, p.exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		Timestamp:    timeNow(),
		DeliveryMode: amqp.Persistent,
	})
}

func (p *Publisher) Close() {
	if p.ch != nil {
		_ = p.ch.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
}

// RoutingKey is a small helper so callers don't hand-build keys with subtle typos.
func RoutingKey(tenant, kind, event string) string {
	return fmt.Sprintf("%s.%s.%s", tenant, kind, event)
}

// indirected for tests
var timeNow = func() time.Time { return time.Now().UTC() }
