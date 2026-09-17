package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"example.com/terraform-k8s-e2e-reference/pkg/events"
	"github.com/nats-io/nats.go"
)

type Bus struct {
	Conn    *nats.Conn
	JS      nats.JetStreamContext
	healthy atomic.Bool
}

func Connect() (*Bus, error) {
	nc, err := nats.Connect(Required("NATS_URL"), nats.Name(Env("SERVICE_NAME", "service")), nats.Timeout(3*time.Second), nats.MaxReconnects(-1), nats.ReconnectWait(250*time.Millisecond))
	if err != nil {
		return nil, fmt.Errorf("connect NATS: %w", err)
	}
	js, err := nc.JetStream(nats.MaxWait(3 * time.Second))
	if err != nil {
		nc.Close()
		return nil, err
	}
	_, err = js.AddStream(&nats.StreamConfig{Name: events.Stream, Subjects: []string{events.Created, events.Resolved, events.Failed}, Storage: nats.FileStorage, Retention: nats.LimitsPolicy, MaxAge: 24 * time.Hour, Duplicates: 2 * time.Minute})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("ensure stream: %w", err)
	}
	b := &Bus{Conn: nc, JS: js}
	b.healthy.Store(true)
	return b, nil
}
func (b *Bus) Ready(context.Context) error {
	if !b.Conn.IsConnected() || !b.healthy.Load() {
		return errors.New("NATS or consumer unavailable")
	}
	return nil
}
func (b *Bus) Close() {
	if err := b.Conn.Drain(); err != nil {
		slog.Error("NATS drain", "error", err)
	}
	b.Conn.Close()
}
func (b *Bus) Publish(ctx context.Context, subject string, event events.Envelope) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = b.JS.Publish(subject, data, nats.MsgId(event.ID), nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}
func (b *Bus) Subscribe(subject, durable string) (*nats.Subscription, error) {
	return b.JS.PullSubscribe(subject, durable, nats.BindStream(events.Stream), nats.AckExplicit(), nats.AckWait(15*time.Second), nats.MaxAckPending(1))
}

// Infrastructure failures remain unacknowledged and are retried. Business failures
// are published as terminal events by the handler and are then acknowledged.
func (b *Bus) Consume(ctx context.Context, sub *nats.Subscription, handle func(context.Context, events.Envelope) error) {
	for ctx.Err() == nil {
		fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		messages, err := sub.Fetch(1, nats.Context(fetchCtx))
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
				b.healthy.Store(true)
				continue
			}
			b.healthy.Store(false)
			slog.Error("fetch failed", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		b.healthy.Store(true)
		for _, msg := range messages {
			var event events.Envelope
			if err := json.Unmarshal(msg.Data, &event); err != nil || event.Version != 1 || event.ID == "" || event.OrderID == "" {
				slog.Error("invalid event terminated", "subject", msg.Subject, "error", err)
				if err := msg.Term(); err != nil {
					slog.Error("terminate message", "error", err)
				}
				continue
			}
			slog.Info("event received", "event", msg.Subject, "event_id", event.ID, "order_id", event.OrderID, "correlation_id", event.CorrelationID)
			if err := handle(ctx, event); err != nil {
				slog.Error("processing failed; will redeliver", "order_id", event.OrderID, "error", err)
				if ctx.Err() == nil {
					if err := msg.NakWithDelay(time.Second); err != nil {
						slog.Error("nak", "error", err)
					}
				}
				continue
			}
			if err := msg.AckSync(nats.Context(ctx)); err != nil {
				slog.Error("ack failed; safe to redeliver", "error", err)
			}
		}
	}
}
