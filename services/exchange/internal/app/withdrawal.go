package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"example.com/terraform-k8s-e2e-reference/services/exchange/internal/provider"
	"example.com/terraform-k8s-e2e-reference/services/exchange/internal/security"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// Withdrawal lifecycle states.
const (
	wCreated         = "CREATED"
	wRequires2FA     = "REQUIRES_2FA"
	wProviderPending = "PROVIDER_PENDING"
	wCompleted       = "COMPLETED"
	wFailed          = "FAILED"
)

type withdrawInput struct {
	Idem          string `json:"idem"`
	Asset         string `json:"asset"`
	Amount        string `json:"amount"`
	To            string `json:"to"`
	Network       string `json:"network"`
	TwoFactorCode string `json:"twoFactorCode"`
}

func (in withdrawInput) validate() error {
	id, err := uuid.Parse(in.Idem)
	if err != nil || id.Version() != 4 {
		return newError(http.StatusBadRequest, codeInvalidRequest, "idem must be a UUIDv4")
	}
	if !symbolPattern.MatchString(in.Asset) {
		return newError(http.StatusBadRequest, codeInvalidRequest, "asset is invalid")
	}
	amount, err := decimal.NewFromString(in.Amount)
	if err != nil || !amount.IsPositive() || !amountPattern.MatchString(in.Amount) {
		return newError(http.StatusBadRequest, codeInvalidRequest, "amount must be a positive decimal")
	}
	if in.To == "" || len(in.To) > 256 {
		return newError(http.StatusBadRequest, codeInvalidRequest, "destination is required")
	}
	if in.Network == "" || len(in.Network) > 64 {
		return newError(http.StatusBadRequest, codeInvalidRequest, "network is required")
	}
	if len(in.TwoFactorCode) > 16 {
		return newError(http.StatusBadRequest, codeInvalidRequest, "twoFactorCode is invalid")
	}
	return nil
}

// withdraw is the provider-neutral withdrawal endpoint (Coinbase "Send Crypto").
func (s *service) withdraw(w http.ResponseWriter, r *http.Request) {
	correlation := correlationOf(r)
	prov, err := s.provider(r.PathValue("exchange"))
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	accountID := r.PathValue("accountId")
	if _, err := uuid.Parse(accountID); err != nil {
		writeError(w, correlation, newError(http.StatusBadRequest, codeInvalidAccountID, "accountId must be a UUID"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var in withdrawInput
	if err := decoder.Decode(&in); err != nil {
		writeError(w, correlation, newError(http.StatusBadRequest, codeInvalidRequest, "invalid withdrawal JSON"))
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, correlation, newError(http.StatusBadRequest, codeInvalidRequest, "unexpected trailing content"))
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, correlation, err)
		return
	}

	ctx := provider.WithCorrelation(r.Context(), correlation)
	status, body, err := s.processWithdrawal(ctx, prov, accountID, in, correlation)
	if err != nil {
		writeError(w, correlation, err)
		return
	}
	writeJSON(w, status, body)
}

// processWithdrawal runs the whole decision under a per-account row lock so that
// idempotency, the local balance pre-check, an optional one-time token refresh and the
// provider send are serialized. Holding the transaction across the provider call keeps
// the single-flight guarantees simple; this is acceptable for a single-replica sample.
func (s *service) processWithdrawal(ctx context.Context, prov provider.Provider, accountID string, in withdrawInput, correlation string) (int, map[string]any, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback(ctx)

	// Lock the connection row: serializes all withdrawals for this account.
	var conn connection
	err = tx.QueryRow(ctx,
		`SELECT id, access_token_ciphertext, refresh_token_ciphertext, token_expires_at
		 FROM cex_accounts WHERE account_id = $1 AND exchange = $2 FOR UPDATE`,
		accountID, prov.Name()).Scan(&conn.ID, &conn.AccessTokenCipher, &conn.RefreshCipher, &conn.TokenExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, newError(http.StatusNotFound, codeConnectionNotFound, "no connected "+prov.Name()+" account for this account")
	}
	if err != nil {
		return 0, nil, err
	}

	// Resolve the provider account holding the requested asset, and its balance.
	var providerAccountID, balanceAmount string
	err = tx.QueryRow(ctx,
		`SELECT provider_account_id, amount::text FROM cex_balances WHERE cex_account_id = $1 AND asset = $2`,
		conn.ID, in.Asset).Scan(&providerAccountID, &balanceAmount)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, newError(http.StatusNotFound, codeAssetNotFound, "no "+in.Asset+" account is connected")
	}
	if err != nil {
		return 0, nil, err
	}

	normAmount := normalizeAmount(in.Amount)
	requestHash := requestFingerprint(providerAccountID, in.Asset, normAmount, in.To, in.Network)

	// Inspect any prior attempt under the same idempotency key.
	var existingID, existingStatus, existingHash, existingTxID string
	err = tx.QueryRow(ctx,
		`SELECT id, status, request_hash, COALESCE(provider_transaction_id,'')
		 FROM cex_withdrawals WHERE cex_account_id = $1 AND idem = $2 FOR UPDATE`,
		conn.ID, in.Idem).Scan(&existingID, &existingStatus, &existingHash, &existingTxID)
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, err
	}

	if found {
		if existingHash != requestHash {
			// Same idem, different business request: never replay money movement.
			return 0, nil, newError(http.StatusConflict, codeIdempotencyConflict, "idem was already used for a different request")
		}
		switch existingStatus {
		case wProviderPending, wCompleted:
			// Already accepted by the provider: return the stored result, no new call.
			return http.StatusOK, withdrawalBody(existingID, prov.Name(), in, existingStatus, existingTxID), nil
		case wFailed:
			return http.StatusOK, withdrawalBody(existingID, prov.Name(), in, existingStatus, existingTxID), nil
		case wRequires2FA:
			if in.TwoFactorCode == "" {
				// Still awaiting the code; re-issue the same challenge without calling out.
				return 0, nil, twoFactorError(in.Idem)
			}
			// Fall through to replay the same request with the 2FA token.
		}
	} else {
		// First attempt: local risk check BEFORE any provider call.
		requested, _ := decimal.NewFromString(in.Amount)
		available, _ := decimal.NewFromString(balanceAmount)
		if requested.GreaterThan(available) {
			return 0, nil, newError(http.StatusUnprocessableEntity, codeInsufficientBalance,
				"requested amount exceeds available "+in.Asset+" balance")
		}
		existingID = uuid.NewString()
		if _, err = tx.Exec(ctx,
			`INSERT INTO cex_withdrawals
			   (id, cex_account_id, idem, request_hash, provider_account_id, asset, amount, destination, network, status, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7::numeric,$8,$9,$10, now(), now())`,
			existingID, conn.ID, in.Idem, requestHash, providerAccountID, in.Asset, normAmount, in.To, in.Network, wCreated); err != nil {
			return 0, nil, err
		}
	}

	// Refresh an expiring access token once, under the same lock (no refresh race).
	accessToken, err := s.accessTokenForSend(ctx, tx, prov, conn)
	if err != nil {
		return 0, nil, err
	}

	result, sendErr := prov.Send(ctx, accessToken, provider.SendRequest{
		ProviderAccountID: providerAccountID,
		To:                in.To,
		Amount:            normAmount,
		Currency:          in.Asset,
		Network:           in.Network,
		Idem:              in.Idem,
		TwoFactorCode:     in.TwoFactorCode,
	})
	if sendErr != nil {
		return s.handleSendError(ctx, tx, existingID, prov.Name(), in, sendErr)
	}

	status := wProviderPending
	if result.Status == "completed" || result.Status == "confirmed" {
		status = wCompleted
	}
	if _, err = tx.Exec(ctx,
		`UPDATE cex_withdrawals SET status=$2, provider_transaction_id=$3, provider_response=$4::jsonb, updated_at=now() WHERE id=$1`,
		existingID, status, result.ProviderTransactionID, providerResponse(result.Raw)); err != nil {
		return 0, nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, nil, err
	}
	slog.Info("withdrawal accepted", "correlation_id", correlation, "account_id", accountID, "cex_account_id", conn.ID,
		"provider_account_id", providerAccountID, "idem", in.Idem, "asset", in.Asset, "operation", "withdrawal",
		"status", status, "provider_transaction_id", result.ProviderTransactionID)
	return http.StatusCreated, withdrawalBody(existingID, prov.Name(), in, status, result.ProviderTransactionID), nil
}

// handleSendError commits the appropriate terminal/awaiting state and surfaces a
// normalized error. A 2FA challenge is not a failure: it persists REQUIRES_2FA so the
// same idem can be replayed with a code.
func (s *service) handleSendError(ctx context.Context, tx pgx.Tx, id, exchange string, in withdrawInput, sendErr error) (int, map[string]any, error) {
	var pe *provider.Error
	if errors.As(sendErr, &pe) {
		switch pe.Code {
		case provider.CodeTwoFactorRequired:
			if _, err := tx.Exec(ctx, `UPDATE cex_withdrawals SET status=$2, updated_at=now() WHERE id=$1`, id, wRequires2FA); err != nil {
				return 0, nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return 0, nil, err
			}
			return 0, nil, twoFactorError(in.Idem)
		case provider.CodeInvalidTwoFactor:
			if _, err := tx.Exec(ctx, `UPDATE cex_withdrawals SET status=$2, updated_at=now() WHERE id=$1`, id, wRequires2FA); err != nil {
				return 0, nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return 0, nil, err
			}
			return 0, nil, newError(http.StatusUnprocessableEntity, codeInvalidTwoFactor, "the two-factor code was not accepted").with("idem", in.Idem).with("retryable", true)
		}
	}
	// A genuine provider failure: record it terminally, then surface a normalized error.
	if _, err := tx.Exec(ctx, `UPDATE cex_withdrawals SET status=$2, updated_at=now() WHERE id=$1`, id, wFailed); err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, err
	}
	return 0, nil, sendErr
}

// accessTokenForSend decrypts the stored access token, refreshing it once if it is
// within 30s of expiry and persisting the (possibly rotated) tokens inside tx.
func (s *service) accessTokenForSend(ctx context.Context, tx pgx.Tx, prov provider.Provider, conn connection) (string, error) {
	accessToken, err := s.cipher.Decrypt(conn.AccessTokenCipher)
	if err != nil {
		return "", err
	}
	if conn.TokenExpiresAt == nil || time.Until(*conn.TokenExpiresAt) > 30*time.Second {
		return accessToken, nil
	}
	refreshToken, err := s.cipher.Decrypt(conn.RefreshCipher)
	if err != nil {
		return "", err
	}
	token, err := prov.RefreshAccessToken(ctx, refreshToken)
	if err != nil {
		return "", err
	}
	accessCipher, err := s.cipher.Encrypt(token.AccessToken)
	if err != nil {
		return "", err
	}
	refreshCipher := conn.RefreshCipher
	if token.RefreshToken != "" {
		if refreshCipher, err = s.cipher.Encrypt(token.RefreshToken); err != nil {
			return "", err
		}
	}
	if _, err = tx.Exec(ctx,
		`UPDATE cex_accounts SET access_token_ciphertext=$2, refresh_token_ciphertext=$3, token_expires_at=$4, updated_at=now() WHERE id=$1`,
		conn.ID, accessCipher, refreshCipher, time.Now().Add(time.Duration(token.ExpiresIn)*time.Second)); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

func twoFactorError(idem string) *apiError {
	return newError(http.StatusPaymentRequired, codeTwoFactorRequired, "Coinbase requires two-factor authentication").
		with("retryable", true).with("idem", idem)
}

func withdrawalBody(id, exchange string, in withdrawInput, status, providerTxID string) map[string]any {
	amount, _ := decimal.NewFromString(in.Amount)
	body := map[string]any{
		"id":       id,
		"idem":     in.Idem,
		"exchange": exchange,
		"asset":    in.Asset,
		"amount":   formatAssetAmount(in.Asset, amount),
		"network":  in.Network,
		"to":       in.To,
		"status":   status,
	}
	if providerTxID != "" {
		body["providerTransactionId"] = providerTxID
	}
	return body
}

func providerResponse(raw json.RawMessage) string {
	if len(raw) == 0 || !json.Valid(raw) {
		return "{}"
	}
	return string(raw)
}

// requestFingerprint is the stable business-request hash. The 2FA code is deliberately
// excluded so "same idem, no 2FA" and "same idem, with 2FA" share one fingerprint.
func requestFingerprint(providerAccountID, asset, amount, to, network string) string {
	return security.RequestHash(providerAccountID, asset, amount, to, network)
}
