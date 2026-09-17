package app

import (
	"testing"
	"time"
)

func TestParsePriceContracts(t *testing.T) {
	tests := []struct {
		name, exchange, body, want string
		valid                      bool
	}{
		{"coinbase", "coinbase", `{"price":"67500.00"}`, "67500.00", true},
		{"kraken", "kraken", `{"error":[],"result":{"XXBTZUSD":{"c":["67500.00","1"]}}}`, "67500.00", true},
		{"kucoin", "kucoin", `{"code":"200000","data":{"price":"67500.00"}}`, "67500.00", true},
		{"malformed JSON", "coinbase", `{`, "", false},
		{"missing price", "coinbase", `{}`, "", false},
		{"zero price", "coinbase", `{"price":"0"}`, "", false},
		{"negative price", "coinbase", `{"price":"-1"}`, "", false},
		{"nondecimal", "coinbase", `{"price":"NaN"}`, "", false},
		{"exponent", "coinbase", `{"price":"1e6"}`, "", false},
		{"excess precision", "coinbase", `{"price":"1.123456789"}`, "", false},
		{"provider error", "kraken", `{"error":["EQuery:Unknown asset pair"],"result":{}}`, "", false},
		{"ambiguous pair", "kraken", `{"error":[],"result":{"A":{"c":["1"]},"B":{"c":["2"]}}}`, "", false},
		{"kucoin error", "kucoin", `{"code":"400100","data":{"price":"1"}}`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePrice(tt.exchange, []byte(tt.body))
			if (err == nil) != tt.valid || got != tt.want {
				t.Fatalf("got %q, %v; want %q valid=%v", got, err, tt.want, tt.valid)
			}
		})
	}
}

func TestE2ERejectsPublicUpstreams(t *testing.T) {
	t.Setenv("APP_MODE", "e2e")
	t.Setenv("COINBASE_BASE_URL", "https://api.exchange.coinbase.com")
	t.Setenv("KRAKEN_BASE_URL", "http://wiremock.e2e.svc.cluster.local:8080/kraken")
	t.Setenv("KUCOIN_BASE_URL", "http://wiremock.e2e.svc.cluster.local:8080/kucoin")
	if _, err := newExchangeClient(); err == nil {
		t.Fatal("accepted public upstream in E2E mode")
	}
	t.Setenv("COINBASE_BASE_URL", "http://wiremock.e2e.svc.cluster.local:8080/coinbase")
	if _, err := newExchangeClient(); err != nil {
		t.Fatal(err)
	}
}

func TestRetryAfterBound(t *testing.T) {
	for value, want := range map[string]time.Duration{"": 0, "nonsense": 0, "-1": 0, "0": 0, "1": time.Second, "3600": time.Second} {
		if got := retryAfter(value); got != want {
			t.Errorf("%q: %v != %v", value, got, want)
		}
	}
}
