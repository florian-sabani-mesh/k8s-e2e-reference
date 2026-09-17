package app

import (
	"context"
	"example.com/terraform-k8s-e2e-reference/pkg/events"
	"example.com/terraform-k8s-e2e-reference/pkg/runtime"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net/http"
	"sync"
)

func Run(ctx context.Context) error {
	db, err := pgxpool.New(ctx, runtime.Required("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	bus, err := runtime.Connect()
	if err != nil {
		return err
	}
	defer bus.Close()
	sub, err := bus.Subscribe("price.*", "settlement-v1")
	if err != nil {
		return err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		bus.Consume(workerCtx, sub, func(ctx context.Context, e events.Envelope) error { return settle(ctx, db, e) })
	}()
	defer func() { cancel(); wg.Wait() }()
	return runtime.Serve(ctx, http.NewServeMux(), func(ctx context.Context) error {
		if err := bus.Ready(ctx); err != nil {
			return err
		}
		return db.Ping(ctx)
	})
}
func settle(ctx context.Context, db *pgxpool.Pool, e events.Envelope) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	if err = tx.QueryRow(ctx, "SELECT status FROM orders WHERE id=$1 FOR UPDATE", e.OrderID).Scan(&status); err != nil {
		return fmt.Errorf("lock order: %w", err)
	}
	// Record every distinct source event, but apply its business effect only once.
	kind := "PRICE_RESOLVED"
	if e.ErrorCode != "" {
		kind = "PRICE_FAILED"
	}
	if status == "SETTLED" || status == "FAILED" {
		kind = "DUPLICATE_IGNORED"
	}
	tag, err := tx.Exec(ctx, "INSERT INTO order_activity(event_id,order_id,kind) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", e.ID, e.OrderID, kind)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	if status == "PENDING" {
		if e.ErrorCode != "" {
			_, err = tx.Exec(ctx, "UPDATE orders SET status='FAILED',error_code=$2 WHERE id=$1", e.OrderID, e.ErrorCode)
		} else {
			_, err = tx.Exec(ctx, "UPDATE orders SET status='PRICE_RESOLVED' WHERE id=$1", e.OrderID)
			if err == nil {
				_, err = tx.Exec(ctx, "INSERT INTO settlements(order_id,id,price,total) SELECT id,$2,$3::numeric,amount*$3::numeric FROM orders WHERE id=$1 ON CONFLICT(order_id) DO NOTHING", e.OrderID, uuid.NewString(), e.Price)
			}
			if err == nil {
				_, err = tx.Exec(ctx, "UPDATE orders SET status='SETTLED' WHERE id=$1", e.OrderID)
			}
			if err == nil {
				_, err = tx.Exec(ctx, "INSERT INTO order_activity(event_id,order_id,kind) VALUES($1,$2,'SETTLED')", e.ID+":settled", e.OrderID)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("settle: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	slog.Info("settlement processed", "order_id", e.OrderID, "correlation_id", e.CorrelationID, "event", kind)
	return nil
}
