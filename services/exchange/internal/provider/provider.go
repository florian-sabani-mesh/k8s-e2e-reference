// Package provider defines a narrow, provider-neutral exchange abstraction so that
// Coinbase-specific HTTP details never leak into the service's handlers or storage.
// Coinbase is implemented today; Kraken and KuCoin are explicitly unsupported and
// return a normalized error rather than a partial fake.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
)

// Token is the OAuth token set returned by a provider. Refresh tokens may rotate,
// so callers must persist whatever comes back, not just the access token.
type Token struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	ExpiresIn    int
}

// User is the minimal, non-sensitive identity we retain about a connected account.
type User struct {
	ID             string
	Name           string
	NativeCurrency string
	CountryCode    string
}

// Account is one asset wallet at the provider (e.g. a BTC wallet or a USD balance).
type Account struct {
	ID      string
	Name    string
	Type    string
	Asset   string
	Amount  string
	Primary bool
}

// SendRequest is a provider-neutral crypto send. The 2FA code is transport-only: it
// is never part of the persisted business request and never logged.
type SendRequest struct {
	ProviderAccountID string
	To                string
	Amount            string
	Currency          string
	Network           string
	Idem              string
	TwoFactorCode     string
}

// SendResult captures the provider's acknowledgement. Status reflects the provider's
// own lifecycle (e.g. "pending"); a pending send is not a completed transfer.
type SendResult struct {
	ProviderTransactionID string
	Status                string
	Raw                   json.RawMessage
}

// Provider is the surface every exchange integration implements.
type Provider interface {
	Name() string
	AuthorizationURL(state, codeChallenge string) string
	ExchangeAuthorizationCode(ctx context.Context, code, codeVerifier string) (Token, error)
	RefreshAccessToken(ctx context.Context, refreshToken string) (Token, error)
	GetUser(ctx context.Context, accessToken string) (User, error)
	ListAccounts(ctx context.Context, accessToken string) ([]Account, error)
	Send(ctx context.Context, accessToken string, req SendRequest) (SendResult, error)
}

// Normalized provider error codes surfaced to the service layer.
const (
	CodeTwoFactorRequired  = "TWO_FACTOR_REQUIRED"
	CodeUnsupported        = "UNSUPPORTED_EXCHANGE"
	CodeTokenExchange      = "OAUTH_TOKEN_EXCHANGE_FAILED"
	CodeUpstreamUnavail    = "UPSTREAM_UNAVAILABLE"
	CodeUpstreamInvalid    = "UPSTREAM_RESPONSE_INVALID"
	CodeUpstreamStatus     = "UPSTREAM_STATUS"
	CodeInvalidTwoFactor   = "INVALID_TWO_FACTOR_CODE"
	CodeUpstreamAuthFailed = "UPSTREAM_UNAUTHORIZED"
)

// Error is a normalized provider failure. It deliberately carries a machine code and
// a safe message; it never embeds tokens, secrets or verifiers.
type Error struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Errorf builds a normalized provider Error.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

type correlationKey struct{}

// WithCorrelation attaches a correlation id to the context so provider
// implementations can propagate it as an outbound header.
func WithCorrelation(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, correlationKey{}, id)
}

// Correlation returns the correlation id attached to ctx, or "".
func Correlation(ctx context.Context) string {
	if id, ok := ctx.Value(correlationKey{}).(string); ok {
		return id
	}
	return ""
}
