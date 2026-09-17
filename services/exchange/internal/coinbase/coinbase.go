// Package coinbase implements provider.Provider against the Coinbase App OAuth2 and
// v2 APIs. Endpoints are fully configurable so the E2E environment can point every
// outbound call at WireMock. The Coinbase "Send Crypto" API models our withdrawal.
package coinbase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"example.com/terraform-k8s-e2e-reference/services/exchange/internal/provider"
)

// Config holds the fully externalized Coinbase endpoints and credentials.
type Config struct {
	AuthorizeURL string
	TokenURL     string
	APIBaseURL   string
	ClientID     string
	ClientSecret string
	CallbackURL  string
	Scopes       []string
	Version      string
}

// Provider is the Coinbase implementation of provider.Provider.
type Provider struct {
	cfg     Config
	http    *http.Client
	retries int
}

// New builds a Coinbase provider. The http.Client carries the request timeout used
// for reads; money-moving sends are never retried (see Send).
func New(cfg Config, client *http.Client, retries int) *Provider {
	if cfg.Version == "" {
		cfg.Version = "2024-11-01"
	}
	if retries < 1 {
		retries = 1
	}
	return &Provider{cfg: cfg, http: client, retries: retries}
}

func (p *Provider) Name() string { return "coinbase" }

// AuthorizationURL builds the Coinbase authorize URL. Scopes are comma separated as
// the Coinbase App OAuth2 reference specifies (not the space-separated OAuth default).
func (p *Provider) AuthorizationURL(state, codeChallenge string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", p.cfg.CallbackURL)
	q.Set("state", state)
	q.Set("scope", strings.Join(p.cfg.Scopes, ","))
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	sep := "?"
	if strings.Contains(p.cfg.AuthorizeURL, "?") {
		sep = "&"
	}
	return p.cfg.AuthorizeURL + sep + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// ExchangeAuthorizationCode performs the PKCE authorization-code token exchange using
// form encoding, as Coinbase requires.
func (p *Provider) ExchangeAuthorizationCode(ctx context.Context, code, codeVerifier string) (provider.Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	form.Set("redirect_uri", p.cfg.CallbackURL)
	form.Set("code_verifier", codeVerifier)
	return p.token(ctx, form)
}

// RefreshAccessToken exchanges a (possibly rotating) refresh token for a new token set.
func (p *Provider) RefreshAccessToken(ctx context.Context, refreshToken string) (provider.Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	return p.token(ctx, form)
}

func (p *Provider) token(ctx context.Context, form url.Values) (provider.Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return provider.Token{}, provider.Errorf(provider.CodeTokenExchange, "build token request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	p.tag(req)
	resp, body, err := p.do(req)
	if err != nil {
		return provider.Token{}, provider.Errorf(provider.CodeUpstreamUnavail, "token endpoint unreachable: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return provider.Token{}, provider.Errorf(provider.CodeTokenExchange, "token endpoint returned HTTP %d", resp.StatusCode)
	}
	var parsed tokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return provider.Token{}, provider.Errorf(provider.CodeUpstreamInvalid, "decode token response: %v", err)
	}
	if parsed.AccessToken == "" {
		return provider.Token{}, provider.Errorf(provider.CodeUpstreamInvalid, "token response missing access_token")
	}
	return provider.Token{
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		TokenType:    parsed.TokenType,
		Scope:        parsed.Scope,
		ExpiresIn:    parsed.ExpiresIn,
	}, nil
}

type userResponse struct {
	Data struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		NativeCurrency string `json:"native_currency"`
		Country        struct {
			Code string `json:"code"`
		} `json:"country"`
	} `json:"data"`
}

// GetUser fetches the connected Coinbase user's non-sensitive metadata.
func (p *Provider) GetUser(ctx context.Context, accessToken string) (provider.User, error) {
	body, err := p.authGet(ctx, accessToken, "/v2/user")
	if err != nil {
		return provider.User{}, err
	}
	var parsed userResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return provider.User{}, provider.Errorf(provider.CodeUpstreamInvalid, "decode user: %v", err)
	}
	if parsed.Data.ID == "" {
		return provider.User{}, provider.Errorf(provider.CodeUpstreamInvalid, "user response missing id")
	}
	return provider.User{
		ID:             parsed.Data.ID,
		Name:           parsed.Data.Name,
		NativeCurrency: parsed.Data.NativeCurrency,
		CountryCode:    parsed.Data.Country.Code,
	}, nil
}

type accountsResponse struct {
	Pagination struct {
		NextStartingAfter string `json:"next_starting_after"`
	} `json:"pagination"`
	Data []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Primary  bool   `json:"primary"`
		Type     string `json:"type"`
		Currency struct {
			Code string `json:"code"`
		} `json:"currency"`
		Balance struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"balance"`
	} `json:"data"`
}

// ListAccounts returns every accessible asset account, following Coinbase pagination.
// A single Coinbase connection commonly exposes many wallets, so we never assume one.
func (p *Provider) ListAccounts(ctx context.Context, accessToken string) ([]provider.Account, error) {
	var accounts []provider.Account
	path := "/v2/accounts"
	for page := 0; page < 50; page++ {
		body, err := p.authGet(ctx, accessToken, path)
		if err != nil {
			return nil, err
		}
		var parsed accountsResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, provider.Errorf(provider.CodeUpstreamInvalid, "decode accounts: %v", err)
		}
		for _, item := range parsed.Data {
			asset := item.Currency.Code
			if asset == "" {
				asset = item.Balance.Currency
			}
			accounts = append(accounts, provider.Account{
				ID:      item.ID,
				Name:    item.Name,
				Type:    item.Type,
				Asset:   asset,
				Amount:  item.Balance.Amount,
				Primary: item.Primary,
			})
		}
		if parsed.Pagination.NextStartingAfter == "" {
			return accounts, nil
		}
		path = "/v2/accounts?starting_after=" + url.QueryEscape(parsed.Pagination.NextStartingAfter)
	}
	return nil, provider.Errorf(provider.CodeUpstreamInvalid, "accounts pagination did not terminate")
}

type sendRequestBody struct {
	Type     string `json:"type"`
	To       string `json:"to"`
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Idem     string `json:"idem"`
	Network  string `json:"network,omitempty"`
}

type sendResponse struct {
	Data struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Status string `json:"status"`
		Amount struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"amount"`
	} `json:"data"`
}

type errorEnvelope struct {
	Errors []struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"errors"`
}

// Send performs a Coinbase "Send Crypto" transaction. It is NOT retried: this is a
// money-moving POST, and although Coinbase's idem key makes a replay safe, we prefer
// explicit caller-driven retries over silent ones. A 2FA challenge is surfaced as a
// normalized retryable error so the caller can resupply the request with a code.
func (p *Provider) Send(ctx context.Context, accessToken string, in provider.SendRequest) (provider.SendResult, error) {
	payload, err := json.Marshal(sendRequestBody{
		Type:     "send",
		To:       in.To,
		Amount:   in.Amount,
		Currency: in.Currency,
		Idem:     in.Idem,
		Network:  in.Network,
	})
	if err != nil {
		return provider.SendResult{}, provider.Errorf(provider.CodeUpstreamInvalid, "encode send: %v", err)
	}
	endpoint := p.cfg.APIBaseURL + "/v2/accounts/" + url.PathEscape(in.ProviderAccountID) + "/transactions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return provider.SendResult{}, provider.Errorf(provider.CodeUpstreamUnavail, "build send request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("CB-VERSION", p.cfg.Version)
	if in.TwoFactorCode != "" {
		// Transport-only header; the 2FA code is never persisted or logged.
		req.Header.Set("CB-2FA-TOKEN", in.TwoFactorCode)
	}
	p.tag(req)
	resp, body, err := p.do(req)
	if err != nil {
		return provider.SendResult{}, provider.Errorf(provider.CodeUpstreamUnavail, "send unreachable: %v", err)
	}
	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
		var parsed sendResponse
		if err := json.Unmarshal(body, &parsed); err != nil || parsed.Data.ID == "" {
			return provider.SendResult{}, provider.Errorf(provider.CodeUpstreamInvalid, "decode send response")
		}
		return provider.SendResult{
			ProviderTransactionID: parsed.Data.ID,
			Status:                parsed.Data.Status,
			Raw:                   json.RawMessage(body),
		}, nil
	case resp.StatusCode == http.StatusPaymentRequired:
		if hasError(body, "two_factor_required") {
			if in.TwoFactorCode != "" {
				// A code was supplied yet Coinbase still demands 2FA: reject as invalid.
				return provider.SendResult{}, &provider.Error{Code: provider.CodeInvalidTwoFactor, Message: "two-factor code was not accepted", Retryable: true}
			}
			return provider.SendResult{}, &provider.Error{Code: provider.CodeTwoFactorRequired, Message: "two-factor authentication required", Retryable: true}
		}
		return provider.SendResult{}, provider.Errorf(provider.CodeUpstreamStatus, "send returned HTTP 402")
	case resp.StatusCode == http.StatusUnauthorized:
		return provider.SendResult{}, provider.Errorf(provider.CodeUpstreamAuthFailed, "send unauthorized")
	default:
		return provider.SendResult{}, provider.Errorf(provider.CodeUpstreamStatus, "send returned HTTP %d", resp.StatusCode)
	}
}

// authGet performs an authenticated GET, retrying only transient failures. Reads are
// idempotent, so bounded retries are safe here (unlike Send).
func (p *Provider) authGet(ctx context.Context, accessToken, path string) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= p.retries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.APIBaseURL+path, nil)
		if err != nil {
			return nil, provider.Errorf(provider.CodeUpstreamUnavail, "build request: %v", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("CB-VERSION", p.cfg.Version)
		p.tag(req)
		resp, body, err := p.do(req)
		if err != nil {
			lastErr = provider.Errorf(provider.CodeUpstreamUnavail, "%s unreachable: %v", path, err)
		} else if resp.StatusCode == http.StatusUnauthorized {
			return nil, provider.Errorf(provider.CodeUpstreamAuthFailed, "%s unauthorized", path)
		} else if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = provider.Errorf(provider.CodeUpstreamStatus, "%s returned HTTP %d", path, resp.StatusCode)
		} else if resp.StatusCode != http.StatusOK {
			return nil, provider.Errorf(provider.CodeUpstreamStatus, "%s returned HTTP %d", path, resp.StatusCode)
		} else {
			return body, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt < p.retries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
			}
		}
	}
	return nil, lastErr
}

func (p *Provider) do(req *http.Request) (*http.Response, []byte, error) {
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("read body: %w", err)
	}
	return resp, body, nil
}

func (p *Provider) tag(req *http.Request) {
	if id := provider.Correlation(req.Context()); id != "" {
		req.Header.Set("X-Test-ID", id)
	}
}

func hasError(body []byte, id string) bool {
	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return false
	}
	for _, e := range env.Errors {
		if e.ID == id {
			return true
		}
	}
	return false
}
