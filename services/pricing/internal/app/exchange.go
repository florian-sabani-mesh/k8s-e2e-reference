package app

import (
	"context"
	"encoding/json"
	"example.com/terraform-k8s-e2e-reference/pkg/events"
	"example.com/terraform-k8s-e2e-reference/pkg/runtime"
	"fmt"
	"github.com/shopspring/decimal"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type upstreamError struct {
	Code  string
	Cause error
}

func (e *upstreamError) Error() string { return e.Code + ": " + e.Cause.Error() }
func (e *upstreamError) Unwrap() error { return e.Cause }

type exchangeClient struct {
	http     *http.Client
	bases    map[string]string
	attempts int
	backoff  time.Duration
}

func newExchangeClient() (*exchangeClient, error) {
	attempts, err := strconv.Atoi(runtime.Env("EXCHANGE_MAX_ATTEMPTS", "3"))
	if err != nil || attempts < 1 || attempts > 5 {
		return nil, fmt.Errorf("EXCHANGE_MAX_ATTEMPTS must be 1..5")
	}
	bases := map[string]string{}
	for _, name := range []string{"coinbase", "kraken", "kucoin"} {
		raw := runtime.Required(strings.ToUpper(name) + "_BASE_URL")
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("invalid %s base URL", name)
		}
		if os.Getenv("APP_MODE") == "e2e" && (u.Scheme != "http" || u.Host != "wiremock.e2e.svc.cluster.local:8080" || u.Path != "/"+name) {
			return nil, fmt.Errorf("E2E refuses non-WireMock URL for %s", name)
		}
		bases[name] = strings.TrimRight(raw, "/")
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second, KeepAlive: 30 * time.Second}).DialContext, MaxIdleConns: 16, MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 2 * time.Second}
	return &exchangeClient{http: &http.Client{Timeout: runtime.Duration("EXCHANGE_TIMEOUT", "400ms"), Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, bases: bases, attempts: attempts, backoff: runtime.Duration("EXCHANGE_BACKOFF", "100ms")}, nil
}
func (c *exchangeClient) price(ctx context.Context, e events.Envelope) (string, error) {
	base, ok := c.bases[e.Exchange]
	if !ok {
		return "", &upstreamError{"INVALID_EXCHANGE", fmt.Errorf("unsupported exchange")}
	}
	path := "/products/" + url.PathEscape(e.Asset+"-"+e.Currency) + "/ticker"
	if e.Exchange == "kraken" {
		path = "/0/public/Ticker?pair=" + url.QueryEscape(e.Asset+e.Currency)
	}
	if e.Exchange == "kucoin" {
		path = "/api/v1/market/orderbook/level1?symbol=" + url.QueryEscape(e.Asset+"-"+e.Currency)
	}
	var last error
	for attempt := 1; attempt <= c.attempts; attempt++ {
		slog.Info("exchange request", "order_id", e.OrderID, "correlation_id", e.CorrelationID, "exchange", e.Exchange, "attempt", attempt)
		price, retry, delay, err := c.once(ctx, base+path, e)
		if err == nil {
			return price, nil
		}
		last = err
		if !retry || attempt == c.attempts {
			return "", last
		}
		backoff := min(c.backoff*time.Duration(1<<(attempt-1)), time.Second)
		if delay > backoff {
			backoff = delay
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", last
}
func (c *exchangeClient) once(ctx context.Context, address string, e events.Envelope) (string, bool, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", false, 0, err
	}
	req.Header.Set("X-Test-ID", e.CorrelationID)
	req.Header.Set("X-Order-ID", e.OrderID)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		code := "UPSTREAM_UNAVAILABLE"
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			code = "UPSTREAM_TIMEOUT"
		}
		return "", ctx.Err() == nil, 0, &upstreamError{code, fmt.Errorf("GET exchange: %w", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		retry := resp.StatusCode == 429 || resp.StatusCode == 500 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504
		return "", retry, retryAfter(resp.Header.Get("Retry-After")), &upstreamError{"UPSTREAM_STATUS", fmt.Errorf("HTTP %d", resp.StatusCode)}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil {
		return "", ctx.Err() == nil, 0, &upstreamError{"UPSTREAM_TIMEOUT", fmt.Errorf("read exchange response: %w", err)}
	}
	if len(data) > 65536 {
		return "", false, 0, &upstreamError{"MALFORMED_RESPONSE", fmt.Errorf("response exceeds 64 KiB")}
	}
	price, err := parsePrice(e.Exchange, data)
	if err != nil {
		return "", false, 0, &upstreamError{"MALFORMED_RESPONSE", err}
	}
	return price, false, 0, nil
}
func retryAfter(value string) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil {
		return min(max(time.Duration(seconds)*time.Second, 0), time.Second)
	}
	if at, err := http.ParseTime(value); err == nil {
		return min(max(time.Until(at), 0), time.Second)
	}
	return 0
}

var pricePattern = regexp.MustCompile(`^[0-9]{1,20}(\.[0-9]{1,8})?$`)

func parsePrice(exchange string, data []byte) (string, error) {
	var price string
	switch exchange {
	case "coinbase":
		var body struct {
			Price string `json:"price"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
			return "", fmt.Errorf("decode Coinbase: %w", err)
		}
		price = body.Price
	case "kraken":
		var body struct {
			Error  []string `json:"error"`
			Result map[string]struct {
				Close []string `json:"c"`
			} `json:"result"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
			return "", fmt.Errorf("decode Kraken: %w", err)
		}
		if len(body.Error) > 0 || len(body.Result) != 1 {
			return "", fmt.Errorf("Kraken error or ambiguous ticker")
		}
		for _, ticker := range body.Result {
			if len(ticker.Close) > 0 {
				price = ticker.Close[0]
			}
		}
	case "kucoin":
		var body struct {
			Code string `json:"code"`
			Data struct {
				Price string `json:"price"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
			return "", fmt.Errorf("decode KuCoin: %w", err)
		}
		if body.Code != "200000" {
			return "", fmt.Errorf("KuCoin rejected request")
		}
		price = body.Data.Price
	default:
		return "", fmt.Errorf("unsupported exchange")
	}
	value, err := decimal.NewFromString(price)
	if err != nil || !pricePattern.MatchString(price) || !value.IsPositive() {
		return "", fmt.Errorf("missing, invalid or nonpositive price")
	}
	return price, nil
}
