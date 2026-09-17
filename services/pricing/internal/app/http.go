package app

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"

	"example.com/terraform-k8s-e2e-reference/pkg/events"
)

var symbol = regexp.MustCompile(`^[A-Z0-9]{2,10}$`)

// priceHandler exposes the pricing service's existing exchange-resolution logic over
// HTTP so other services (the exchange service) can obtain spot prices without
// duplicating provider contracts or retries. It reuses the same resilient client the
// asynchronous consumer uses, so retry/timeout behavior is identical.
func priceHandler(client *exchangeClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		asset := r.PathValue("asset")
		quote := r.URL.Query().Get("quote")
		if quote == "" {
			quote = "USD"
		}
		exchange := r.URL.Query().Get("exchange")
		if exchange == "" {
			exchange = "coinbase"
		}
		if !symbol.MatchString(asset) || !symbol.MatchString(quote) {
			http.Error(w, "invalid asset or quote", http.StatusBadRequest)
			return
		}
		correlation := r.Header.Get("X-Test-ID")
		event := events.Envelope{
			Version: 1, ID: "price-query", OrderID: "price-query",
			CorrelationID: correlation, Asset: asset, Currency: quote, Exchange: exchange,
		}
		price, err := client.price(r.Context(), event)
		if err != nil {
			code := "UPSTREAM_ERROR"
			var failure *upstreamError
			if errors.As(err, &failure) {
				code = failure.Code
			}
			slog.Warn("price query failed", "asset", asset, "quote", quote, "exchange", exchange, "correlation_id", correlation, "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"asset": asset, "quote": quote, "exchange": exchange, "price": price,
		})
	}
}
