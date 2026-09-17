package coinbase

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"example.com/terraform-k8s-e2e-reference/services/exchange/internal/provider"
)

func newProvider(t *testing.T, handler http.Handler) *Provider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(Config{
		AuthorizeURL: server.URL + "/oauth2/auth",
		TokenURL:     server.URL + "/oauth2/token",
		APIBaseURL:   server.URL + "/api",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		CallbackURL:  "https://example.test/callback",
		Scopes:       []string{"wallet:user:read", "wallet:transactions:send"},
	}, &http.Client{Timeout: 2 * time.Second}, 3)
}

func TestAuthorizationURLHasPKCEAndScopes(t *testing.T) {
	p := newProvider(t, http.NewServeMux())
	got := p.AuthorizationURL("state-123", "challenge-abc")
	for _, want := range []string{
		"response_type=code", "client_id=client-id", "code_challenge=challenge-abc",
		"code_challenge_method=S256", "state=state-123",
		"scope=wallet%3Auser%3Aread%2Cwallet%3Atransactions%3Asend",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("authorization URL missing %q: %s", want, got)
		}
	}
}

func TestExchangeAuthorizationCodeSendsForm(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("expected form content-type, got %q", ct)
		}
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "the-code" || r.Form.Get("code_verifier") != "the-verifier" {
			t.Errorf("unexpected form: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","token_type":"bearer","expires_in":3600,"scope":"wallet:user:read"}`))
	})
	token, err := newProvider(t, mux).ExchangeAuthorizationCode(context.Background(), "the-code", "the-verifier")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if token.AccessToken != "at" || token.RefreshToken != "rt" || token.ExpiresIn != 3600 {
		t.Fatalf("unexpected token: %+v", token)
	}
}

func TestListAccountsFollowsPagination(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/accounts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("starting_after") == "cursor-1" {
			_, _ = w.Write([]byte(`{"pagination":{"next_starting_after":""},"data":[{"id":"eth","currency":{"code":"ETH"},"balance":{"amount":"2.0","currency":"ETH"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"pagination":{"next_starting_after":"cursor-1"},"data":[{"id":"btc","primary":true,"currency":{"code":"BTC"},"balance":{"amount":"1.5","currency":"BTC"}}]}`))
	})
	accounts, err := newProvider(t, mux).ListAccounts(context.Background(), "at")
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(accounts) != 2 || accounts[0].Asset != "BTC" || accounts[1].Asset != "ETH" {
		t.Fatalf("unexpected accounts: %+v", accounts)
	}
}

func TestSendTranslatesTwoFactorAndSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/accounts/btc/transactions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("CB-2FA-TOKEN") == "123456" {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"tx-1","type":"send","status":"pending","amount":{"amount":"-0.25","currency":"BTC"}}}`))
			return
		}
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"errors":[{"id":"two_factor_required","message":"Two factor authentication required"}]}`))
	})
	p := newProvider(t, mux)
	base := provider.SendRequest{ProviderAccountID: "btc", To: "addr", Amount: "0.25", Currency: "BTC", Idem: "idem-1", Network: "bitcoin"}

	_, err := p.Send(context.Background(), "at", base)
	var perr *provider.Error
	if !errors.As(err, &perr) || perr.Code != provider.CodeTwoFactorRequired {
		t.Fatalf("expected TWO_FACTOR_REQUIRED, got %v", err)
	}

	withCode := base
	withCode.TwoFactorCode = "123456"
	result, err := p.Send(context.Background(), "at", withCode)
	if err != nil {
		t.Fatalf("send with 2FA: %v", err)
	}
	if result.ProviderTransactionID != "tx-1" || result.Status != "pending" {
		t.Fatalf("unexpected send result: %+v", result)
	}
}
