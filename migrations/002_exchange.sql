-- Exchange (CEX) connection, balance snapshot and withdrawal tables.
-- Monetary values use NUMERIC; amounts never touch floating point.

-- One row per OAuth authorization attempt. Only the hash of the state is stored, and
-- the PKCE verifier is stored encrypted. A row is single-use via consumed_at.
CREATE TABLE cex_oauth_sessions
(
    id                       uuid PRIMARY KEY,
    exchange                 text        NOT NULL,
    account_id               uuid        NOT NULL,
    state_hash               text        NOT NULL UNIQUE,
    redirect_url             text        NOT NULL,
    code_verifier_ciphertext text        NOT NULL,
    created_at               timestamptz NOT NULL DEFAULT now(),
    expires_at               timestamptz NOT NULL,
    consumed_at              timestamptz
);
CREATE INDEX cex_oauth_sessions_expiry ON cex_oauth_sessions (expires_at);

-- One connected exchange account per (internal account, exchange). OAuth tokens are
-- stored only as AES-256-GCM ciphertext.
CREATE TABLE cex_accounts
(
    id                       uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    account_id               uuid        NOT NULL,
    exchange                 text        NOT NULL CHECK (exchange IN ('coinbase', 'kraken', 'kucoin')),
    provider_user_id         text        NOT NULL,
    provider_user_name       text        NOT NULL DEFAULT '',
    native_currency          text        NOT NULL DEFAULT '',
    country_code             text        NOT NULL DEFAULT '',
    access_token_ciphertext  text        NOT NULL,
    refresh_token_ciphertext text        NOT NULL,
    token_expires_at         timestamptz,
    granted_scopes           text        NOT NULL DEFAULT '',
    metadata                 jsonb       NOT NULL DEFAULT '{}'::jsonb,
    connected_at             timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    UNIQUE (account_id, exchange)
);

-- Multi-asset balance snapshot captured at connection time. Coinbase exposes many
-- wallets, so each asset account is its own row.
CREATE TABLE cex_balances
(
    id                  uuid PRIMARY KEY        DEFAULT gen_random_uuid(),
    cex_account_id      uuid           NOT NULL REFERENCES cex_accounts (id) ON DELETE CASCADE,
    provider_account_id text           NOT NULL,
    asset               text           NOT NULL,
    account_name        text           NOT NULL DEFAULT '',
    account_type        text           NOT NULL DEFAULT '',
    is_primary          boolean        NOT NULL DEFAULT false,
    amount              numeric(38, 18) NOT NULL DEFAULT 0 CHECK (amount >= 0),
    metadata            jsonb          NOT NULL DEFAULT '{}'::jsonb,
    fetched_at          timestamptz    NOT NULL DEFAULT now(),
    UNIQUE (cex_account_id, provider_account_id)
);
CREATE INDEX cex_balances_account_asset ON cex_balances (cex_account_id, asset);

-- Withdrawals with our own idempotency independent of Coinbase's idem key. The 2FA
-- code is never stored; request_hash excludes it so a 2FA retry matches the original.
CREATE TABLE cex_withdrawals
(
    id                      uuid PRIMARY KEY,
    cex_account_id          uuid           NOT NULL REFERENCES cex_accounts (id) ON DELETE CASCADE,
    idem                    uuid           NOT NULL,
    request_hash            text           NOT NULL,
    provider_account_id     text           NOT NULL,
    asset                   text           NOT NULL,
    amount                  numeric(38, 18) NOT NULL CHECK (amount > 0),
    destination             text           NOT NULL,
    network                 text           NOT NULL DEFAULT '',
    status                  text           NOT NULL CHECK (status IN
                                                           ('CREATED', 'REQUIRES_2FA', 'PROVIDER_PENDING', 'COMPLETED', 'FAILED')),
    provider_transaction_id text,
    provider_response       jsonb          NOT NULL DEFAULT '{}'::jsonb,
    created_at              timestamptz    NOT NULL DEFAULT now(),
    updated_at              timestamptz    NOT NULL DEFAULT now(),
    UNIQUE (cex_account_id, idem)
);
