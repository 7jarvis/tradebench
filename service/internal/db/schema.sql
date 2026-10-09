-- Applied on startup under an advisory lock, so several replicas can start at once.
-- Every statement is idempotent.

CREATE TABLE IF NOT EXISTS accounts (
    id             UUID PRIMARY KEY,
    owner_name     TEXT        NOT NULL,
    status         TEXT        NOT NULL,
    cash_cents     BIGINT      NOT NULL DEFAULT 0,
    -- Cash held by open buy limit orders. buying_power = cash - reserved.
    reserved_cents BIGINT      NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cash_non_negative     CHECK (cash_cents >= 0),
    CONSTRAINT reserved_non_negative CHECK (reserved_cents >= 0),
    CONSTRAINT reserved_within_cash  CHECK (reserved_cents <= cash_cents)
);

-- Sandbox market data: one current price per symbol.
CREATE TABLE IF NOT EXISTS prices (
    symbol      TEXT PRIMARY KEY,
    price_cents BIGINT      NOT NULL CHECK (price_cents > 0),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO prices (symbol, price_cents) VALUES
    ('AAPL', 19000), ('MSFT', 42000), ('NVDA', 12000), ('TSLA', 25000)
ON CONFLICT (symbol) DO NOTHING;

CREATE TABLE IF NOT EXISTS orders (
    id                     UUID PRIMARY KEY,
    account_id             UUID        NOT NULL REFERENCES accounts (id),
    client_order_id        TEXT        NOT NULL,
    symbol                 TEXT        NOT NULL REFERENCES prices (symbol),
    side                   TEXT        NOT NULL,
    type                   TEXT        NOT NULL,
    qty                    BIGINT      NOT NULL CHECK (qty > 0),
    limit_price_cents      BIGINT,
    status                 TEXT        NOT NULL,
    filled_qty             BIGINT      NOT NULL DEFAULT 0,
    filled_avg_price_cents BIGINT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    filled_at              TIMESTAMPTZ,
    canceled_at            TIMESTAMPTZ,
    CONSTRAINT client_order_id_unique UNIQUE (account_id, client_order_id)
);

CREATE INDEX IF NOT EXISTS orders_open_by_symbol ON orders (symbol) WHERE status = 'new';
CREATE INDEX IF NOT EXISTS orders_by_account ON orders (account_id, created_at);

CREATE TABLE IF NOT EXISTS positions (
    account_id            UUID   NOT NULL REFERENCES accounts (id),
    symbol                TEXT   NOT NULL REFERENCES prices (symbol),
    qty                   BIGINT NOT NULL CHECK (qty > 0),
    avg_entry_price_cents BIGINT NOT NULL,
    PRIMARY KEY (account_id, symbol)
);

-- Transactional outbox: events are written in the same transaction as the
-- state change and relayed to Kafka afterwards (at-least-once delivery).
CREATE TABLE IF NOT EXISTS outbox (
    id           BIGSERIAL PRIMARY KEY,
    topic        TEXT        NOT NULL,
    key          TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS outbox_unpublished ON outbox (id) WHERE published_at IS NULL;
