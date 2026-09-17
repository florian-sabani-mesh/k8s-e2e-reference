package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type store struct {
	db *pgxpool.Pool
}

type oauthSession struct {
	ID           string
	Exchange     string
	AccountID    string
	RedirectURL  string
	CodeVerifier string // ciphertext at rest; the caller decrypts
	ExpiresAt    time.Time
}

type connection struct {
	ID                string
	AccountID         string
	Exchange          string
	AccessTokenCipher string
	RefreshCipher     string
	TokenExpiresAt    *time.Time
	NativeCurrency    string
}

type balanceRow struct {
	ProviderAccountID string
	Asset             string
	AccountName       string
	AccountType       string
	Amount            string
	Primary           bool
	FetchedAt         time.Time
}

// createSession persists a new OAuth attempt. Only the hash of the state is stored,
// and the PKCE verifier is stored encrypted.
func (s *store) createSession(ctx context.Context, sess oauthSession, stateHash string) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO cex_oauth_sessions(id, exchange, account_id, state_hash, redirect_url, code_verifier_ciphertext, expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		sess.ID, sess.Exchange, sess.AccountID, stateHash, sess.RedirectURL, sess.CodeVerifier, sess.ExpiresAt)
	return err
}

// consumeSession atomically marks an unexpired, unconsumed session as used and
// returns it. The single UPDATE ... RETURNING is the single-use gate: a replay finds
// no row to consume. A miss is then classified as invalid, expired or already used.
func (s *store) consumeSession(ctx context.Context, exchange, stateHash string) (oauthSession, error) {
	var sess oauthSession
	err := s.db.QueryRow(ctx,
		`UPDATE cex_oauth_sessions SET consumed_at = now()
		 WHERE exchange = $1 AND state_hash = $2 AND consumed_at IS NULL AND expires_at > now()
		 RETURNING id, exchange, account_id, redirect_url, code_verifier_ciphertext, expires_at`,
		exchange, stateHash).Scan(&sess.ID, &sess.Exchange, &sess.AccountID, &sess.RedirectURL, &sess.CodeVerifier, &sess.ExpiresAt)
	if err == nil {
		return sess, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return oauthSession{}, err
	}
	return oauthSession{}, s.classifySessionMiss(ctx, exchange, stateHash)
}

func (s *store) classifySessionMiss(ctx context.Context, exchange, stateHash string) error {
	var consumed *time.Time
	var expiresAt time.Time
	err := s.db.QueryRow(ctx,
		`SELECT consumed_at, expires_at FROM cex_oauth_sessions WHERE exchange = $1 AND state_hash = $2`,
		exchange, stateHash).Scan(&consumed, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return newError(http.StatusBadRequest, codeInvalidOAuthState, "OAuth state is invalid")
	}
	if err != nil {
		return err
	}
	if consumed != nil {
		return newError(http.StatusConflict, codeOAuthStateUsed, "OAuth state has already been used")
	}
	return newError(http.StatusBadRequest, codeOAuthStateExpired, "OAuth state has expired")
}

// connectParams carries everything the callback persists after a successful OAuth.
type connectParams struct {
	AccountID         string
	Exchange          string
	ProviderUserID    string
	ProviderUserName  string
	NativeCurrency    string
	CountryCode       string
	AccessTokenCipher string
	RefreshCipher     string
	TokenExpiresAt    time.Time
	GrantedScopes     string
	Balances          []balanceRow
}

// persistConnection stores the connected account and replaces its balance snapshot in
// one transaction. The unique(account_id, exchange) constraint makes reconnecting an
// upsert rather than a duplicate.
func (s *store) persistConnection(ctx context.Context, p connectParams) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var cexAccountID string
	err = tx.QueryRow(ctx,
		`INSERT INTO cex_accounts
		   (id, account_id, exchange, provider_user_id, provider_user_name, native_currency, country_code,
		    access_token_ciphertext, refresh_token_ciphertext, token_expires_at, granted_scopes, metadata, connected_at, updated_at)
		 VALUES (gen_random_uuid(),$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'{}'::jsonb, now(), now())
		 ON CONFLICT (account_id, exchange) DO UPDATE SET
		   provider_user_id = EXCLUDED.provider_user_id,
		   provider_user_name = EXCLUDED.provider_user_name,
		   native_currency = EXCLUDED.native_currency,
		   country_code = EXCLUDED.country_code,
		   access_token_ciphertext = EXCLUDED.access_token_ciphertext,
		   refresh_token_ciphertext = EXCLUDED.refresh_token_ciphertext,
		   token_expires_at = EXCLUDED.token_expires_at,
		   granted_scopes = EXCLUDED.granted_scopes,
		   updated_at = now()
		 RETURNING id`,
		p.AccountID, p.Exchange, p.ProviderUserID, p.ProviderUserName, p.NativeCurrency, p.CountryCode,
		p.AccessTokenCipher, p.RefreshCipher, p.TokenExpiresAt, p.GrantedScopes).Scan(&cexAccountID)
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM cex_balances WHERE cex_account_id = $1`, cexAccountID); err != nil {
		return "", err
	}
	for _, b := range p.Balances {
		if _, err = tx.Exec(ctx,
			`INSERT INTO cex_balances
			   (id, cex_account_id, provider_account_id, asset, account_name, account_type, is_primary, amount, metadata, fetched_at)
			 VALUES (gen_random_uuid(),$1,$2,$3,$4,$5,$6,$7::numeric,'{}'::jsonb, now())`,
			cexAccountID, b.ProviderAccountID, b.Asset, b.AccountName, b.AccountType, b.Primary, b.Amount); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return cexAccountID, nil
}

func (s *store) getConnection(ctx context.Context, accountID, exchange string) (connection, error) {
	var c connection
	err := s.db.QueryRow(ctx,
		`SELECT id, account_id, exchange, access_token_ciphertext, refresh_token_ciphertext, token_expires_at, native_currency
		 FROM cex_accounts WHERE account_id = $1 AND exchange = $2`,
		accountID, exchange).Scan(&c.ID, &c.AccountID, &c.Exchange, &c.AccessTokenCipher, &c.RefreshCipher, &c.TokenExpiresAt, &c.NativeCurrency)
	if errors.Is(err, pgx.ErrNoRows) {
		return connection{}, newError(http.StatusNotFound, codeConnectionNotFound, "no connected "+exchange+" account for this account")
	}
	if err != nil {
		return connection{}, err
	}
	return c, nil
}

func (s *store) getBalances(ctx context.Context, cexAccountID string) ([]balanceRow, error) {
	rows, err := s.db.Query(ctx,
		`SELECT provider_account_id, asset, account_name, account_type, is_primary, amount::text, fetched_at
		 FROM cex_balances WHERE cex_account_id = $1 ORDER BY asset`, cexAccountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []balanceRow
	for rows.Next() {
		var b balanceRow
		if err := rows.Scan(&b.ProviderAccountID, &b.Asset, &b.AccountName, &b.AccountType, &b.Primary, &b.Amount, &b.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
