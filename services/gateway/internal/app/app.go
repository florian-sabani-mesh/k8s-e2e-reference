package app

import (
	"context"
	"example.com/terraform-k8s-e2e-reference/pkg/runtime"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

func Run(ctx context.Context) error {
	ordersTarget, err := url.Parse(runtime.Required("ORDERS_URL"))
	if err != nil || ordersTarget.Host == "" {
		return fmt.Errorf("invalid ORDERS_URL")
	}
	exchangeTarget, err := url.Parse(runtime.Required("EXCHANGE_URL"))
	if err != nil || exchangeTarget.Host == "" {
		return fmt.Errorf("invalid EXCHANGE_URL")
	}
	timeout := runtime.Duration("UPSTREAM_TIMEOUT", "3s")
	idle := runtime.Duration("IDLE_TIMEOUT", "30s")
	ordersProxy := newProxy(ordersTarget, timeout, idle)
	exchangeProxy := newProxy(exchangeTarget, timeout, idle)

	mux := http.NewServeMux()
	mux.Handle("/orders", ordersProxy)
	mux.Handle("/orders/", ordersProxy)
	// All exchange APIs, including the OAuth callback, flow through the gateway boundary.
	mux.Handle("/exchanges/", exchangeProxy)

	client := &http.Client{Timeout: timeout}
	return runtime.Serve(ctx, mux, func(ctx context.Context) error {
		return probe(ctx, client, ordersTarget.String()+"/readyz")
	})
}

// newProxy builds a single-host reverse proxy with a bounded response timeout and a
// safe 502 error handler that surfaces failures without leaking upstream internals.
func newProxy(target *url.URL, responseTimeout, idle time.Duration) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{
		ResponseHeaderTimeout: responseTimeout,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       idle,
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Error("upstream request failed", "correlation_id", r.Header.Get("X-Test-ID"), "path", r.URL.Path, "error", err)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}
	return proxy
}

func probe(ctx context.Context, client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("orders not ready: %d", resp.StatusCode)
	}
	return nil
}
