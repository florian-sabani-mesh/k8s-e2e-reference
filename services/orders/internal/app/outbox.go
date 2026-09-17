package app

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/terraform-k8s-e2e-reference/pkg/events"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"time"
)

// A crash after publish but before commit republishes the same event ID.
// JetStream deduplicates within its window; downstream business keys are permanent.
func (s *service) dispatch(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.dispatchOne(ctx); err != nil && ctx.Err() == nil {
				slog.Error("outbox dispatch", "error", err)
			}
		}
	}
}
func (s *service) dispatchOne(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id, subject string
	var payload []byte
	err = tx.QueryRow(ctx, "SELECT event_id,subject,payload FROM outbox WHERE published_at IS NULL ORDER BY event_id FOR UPDATE SKIP LOCKED LIMIT 1").Scan(&id, &subject, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var event events.Envelope
	if err = json.Unmarshal(payload, &event); err != nil {
		return err
	}
	if err = s.bus.Publish(ctx, subject, event); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE outbox SET published_at=now() WHERE event_id=$1", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
