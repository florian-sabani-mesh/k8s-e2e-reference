package app

import (
	"net/http"
	"testing"

	"example.com/terraform-k8s-e2e-reference/services/exchange/internal/provider"
	"github.com/shopspring/decimal"
)

func TestAllowedRedirect(t *testing.T) {
	cfg := Config{RedirectAllowlist: []string{"http://localhost:3000", "https://app.example.com"}}
	cases := map[string]bool{
		"http://localhost:3000/settings/exchanges": true,
		"https://app.example.com/done":             true,
		"http://localhost:3000":                    true,
		"http://evil.example.com/steal":            false,
		"https://localhost:3000/x":                 false, // scheme must match
		"ftp://localhost:3000":                     false,
		"not-a-url":                                false,
		"http://localhost:3001/settings":           false,
	}
	for raw, want := range cases {
		if got := cfg.allowedRedirect(raw); got != want {
			t.Errorf("allowedRedirect(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestGuardRejectsPublicCoinbaseInE2E(t *testing.T) {
	cfg := Config{AppMode: "e2e"}
	cfg.Coinbase.TokenURL = "https://login.coinbase.com/oauth2/token"
	cfg.Coinbase.APIBaseURL = "http://wiremock.e2e.svc.cluster.local:8080/coinbase-api"
	cfg.Coinbase.AuthorizeURL = "http://wiremock.e2e.svc.cluster.local:8080/coinbase-oauth/oauth2/auth"
	if err := cfg.guard(); err == nil {
		t.Fatal("expected guard to reject a public Coinbase token host in E2E mode")
	}

	cfg.Coinbase.TokenURL = "http://wiremock.e2e.svc.cluster.local:8080/coinbase-oauth/oauth2/token"
	if err := cfg.guard(); err != nil {
		t.Fatalf("guard rejected an all-WireMock config: %v", err)
	}
}

func TestRequestFingerprintExcludesTwoFactor(t *testing.T) {
	// The fingerprint is computed from business fields only; a 2FA retry must match.
	base := requestFingerprint("btc", "BTC", "0.25", "addr", "bitcoin")
	if base != requestFingerprint("btc", "BTC", "0.25", "addr", "bitcoin") {
		t.Fatal("fingerprint must be deterministic")
	}
	if base == requestFingerprint("btc", "BTC", "0.50", "addr", "bitcoin") {
		t.Fatal("a different amount must change the fingerprint")
	}
	if base == requestFingerprint("btc", "BTC", "0.25", "other-addr", "bitcoin") {
		t.Fatal("a different destination must change the fingerprint")
	}
}

func TestDecimalValuationIsExact(t *testing.T) {
	amount := decimal.RequireFromString("1.5")
	price := decimal.RequireFromString("60000.00")
	if got := amount.Mul(price).StringFixed(2); got != "90000.00" {
		t.Fatalf("valuation = %s, want 90000.00", got)
	}
	total := decimal.Zero
	for _, v := range []string{"90000", "6000", "1000"} {
		total = total.Add(decimal.RequireFromString(v))
	}
	if got := total.StringFixed(2); got != "97000.00" {
		t.Fatalf("total = %s, want 97000.00", got)
	}
	if got := formatAssetAmount("BTC", amount); got != "1.50000000" {
		t.Fatalf("BTC amount = %s, want 1.50000000", got)
	}
	if got := formatAssetAmount("USD", decimal.RequireFromString("1000")); got != "1000.00" {
		t.Fatalf("USD amount = %s, want 1000.00", got)
	}
}

func TestMapProviderErrorStatuses(t *testing.T) {
	cases := map[string]int{
		provider.CodeTwoFactorRequired: http.StatusPaymentRequired,
		provider.CodeInvalidTwoFactor:  http.StatusUnprocessableEntity,
		provider.CodeTokenExchange:     http.StatusBadGateway,
		provider.CodeUpstreamUnavail:   http.StatusBadGateway,
	}
	for code, want := range cases {
		got := mapProviderError(&provider.Error{Code: code})
		if got.Status != want {
			t.Errorf("mapProviderError(%s).Status = %d, want %d", code, got.Status, want)
		}
	}
}
