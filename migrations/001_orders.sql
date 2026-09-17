CREATE TABLE orders
(
    id             uuid PRIMARY KEY,
    correlation_id text           NOT NULL,
    asset          text           NOT NULL,
    currency       text           NOT NULL,
    amount         numeric(28, 8) NOT NULL CHECK (amount > 0),
    exchange       text           NOT NULL CHECK (exchange IN ('coinbase', 'kraken', 'kucoin')),
    status         text           NOT NULL CHECK (status IN ('PENDING', 'PRICE_RESOLVED', 'SETTLED', 'FAILED')),
    error_code     text,
    created_at     timestamptz    NOT NULL DEFAULT now()
);
CREATE TABLE outbox
(
    event_id     uuid PRIMARY KEY,
    subject      text  NOT NULL,
    payload      jsonb NOT NULL,
    published_at timestamptz
);
CREATE INDEX outbox_pending ON outbox (event_id) WHERE published_at IS NULL;
-- Permanent business idempotency, independent of the JetStream deduplication window.
CREATE TABLE settlements
(
    order_id   uuid PRIMARY KEY REFERENCES orders (id),
    id         uuid            NOT NULL UNIQUE,
    price      numeric(28, 8)  NOT NULL CHECK (price > 0),
    total      numeric(56, 16) NOT NULL CHECK (total > 0),
    created_at timestamptz     NOT NULL DEFAULT now()
);
CREATE TABLE order_activity
(
    event_id   text PRIMARY KEY,
    order_id   uuid        NOT NULL REFERENCES orders (id),
    kind       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX order_activity_order ON order_activity (order_id, created_at);
