package app

import (
	"context"
	"errors"
	"example.com/terraform-k8s-e2e-reference/pkg/events"
	"example.com/terraform-k8s-e2e-reference/pkg/runtime"
	"log/slog"
	"net/http"
	"sync"
)

func Run(ctx context.Context) error {
	client, err := newExchangeClient()
	if err != nil {
		return err
	}
	bus, err := runtime.Connect()
	if err != nil {
		return err
	}
	defer bus.Close()
	sub, err := bus.Subscribe(events.Created, "pricing-v1")
	if err != nil {
		return err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		bus.Consume(workerCtx, sub, func(ctx context.Context, event events.Envelope) error {
			price, err := client.price(ctx, event)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			subject := events.Resolved
			event.ID += ":price"
			event.Price = price
			if err != nil {
				subject = events.Failed
				var failure *upstreamError
				if errors.As(err, &failure) {
					event.ErrorCode = failure.Code
				} else {
					event.ErrorCode = "UPSTREAM_ERROR"
				}
				slog.Warn("price failed", "order_id", event.OrderID, "correlation_id", event.CorrelationID, "error", err)
			}
			return bus.Publish(ctx, subject, event)
		})
	}()
	defer func() { cancel(); wg.Wait() }()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /prices/{asset}", priceHandler(client))
	return runtime.Serve(ctx, mux, bus.Ready)
}
