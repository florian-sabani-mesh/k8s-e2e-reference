package app

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"example.com/terraform-k8s-e2e-reference/services/exchange/internal/provider"
)

// Normalized service error codes. These are the machine-readable contract; messages
// are human-readable and safe. Tokens, secrets and verifiers never appear here.
const (
	codeUnsupportedExchange = "UNSUPPORTED_EXCHANGE"
	codeInvalidAccountID    = "INVALID_ACCOUNT_ID"
	codeInvalidRedirect     = "INVALID_REDIRECT_URL"
	codeInvalidOAuthState   = "INVALID_OAUTH_STATE"
	codeOAuthStateExpired   = "OAUTH_STATE_EXPIRED"
	codeOAuthStateUsed      = "OAUTH_STATE_ALREADY_USED"
	codeTokenExchange       = "OAUTH_TOKEN_EXCHANGE_FAILED"
	codeConnectionNotFound  = "EXCHANGE_CONNECTION_NOT_FOUND"
	codeAssetNotFound       = "ASSET_ACCOUNT_NOT_FOUND"
	codeInsufficientBalance = "INSUFFICIENT_BALANCE"
	codeTwoFactorRequired   = "TWO_FACTOR_REQUIRED"
	codeInvalidTwoFactor    = "INVALID_TWO_FACTOR_CODE"
	codeIdempotencyConflict = "IDEMPOTENCY_CONFLICT"
	codeInvalidRequest      = "INVALID_REQUEST"
	codeUpstreamUnavailable = "UPSTREAM_UNAVAILABLE"
	codeUpstreamInvalid     = "UPSTREAM_RESPONSE_INVALID"
	codeInternal            = "INTERNAL"
)

// apiError is a normalized, client-facing error with a machine code, HTTP status and
// optional extra fields (for example the idem and retryable flag on a 2FA challenge).
type apiError struct {
	Status  int
	Code    string
	Message string
	Extra   map[string]any
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func newError(status int, code, message string) *apiError {
	return &apiError{Status: status, Code: code, Message: message}
}

func (e *apiError) with(key string, value any) *apiError {
	if e.Extra == nil {
		e.Extra = map[string]any{}
	}
	e.Extra[key] = value
	return e
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode response", "error", err)
	}
}

// writeError renders any error as the normalized envelope {"error":{...}}. A typed
// apiError is used directly; a provider.Error is mapped to a safe status; anything
// else becomes a generic 500 without leaking internals.
func writeError(w http.ResponseWriter, correlation string, err error) {
	ae := asAPIError(err)
	body := map[string]any{"code": ae.Code, "message": ae.Message}
	for key, value := range ae.Extra {
		body[key] = value
	}
	slog.Warn("request failed", "correlation_id", correlation, "code", ae.Code, "status", ae.Status)
	writeJSON(w, ae.Status, map[string]any{"error": body})
}

func asAPIError(err error) *apiError {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae
	}
	var pe *provider.Error
	if errors.As(err, &pe) {
		return mapProviderError(pe)
	}
	return newError(http.StatusInternalServerError, codeInternal, "internal error")
}

func mapProviderError(pe *provider.Error) *apiError {
	switch pe.Code {
	case provider.CodeTwoFactorRequired:
		return newError(http.StatusPaymentRequired, codeTwoFactorRequired, "Coinbase requires two-factor authentication")
	case provider.CodeInvalidTwoFactor:
		return newError(http.StatusUnprocessableEntity, codeInvalidTwoFactor, "the two-factor code was not accepted")
	case provider.CodeTokenExchange:
		return newError(http.StatusBadGateway, codeTokenExchange, "OAuth token exchange failed")
	case provider.CodeUpstreamInvalid:
		return newError(http.StatusBadGateway, codeUpstreamInvalid, "the exchange returned an invalid response")
	case provider.CodeUnsupported:
		return newError(http.StatusBadRequest, codeUnsupportedExchange, "exchange is not supported")
	default:
		return newError(http.StatusBadGateway, codeUpstreamUnavailable, "the exchange is currently unavailable")
	}
}
