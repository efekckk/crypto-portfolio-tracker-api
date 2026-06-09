CREATE TABLE virtual_portfolios (
    id                UUID PRIMARY KEY,
    device_id         UUID NOT NULL REFERENCES devices(device_id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    starting_balance  DOUBLE PRECISION NOT NULL CHECK (starting_balance > 0),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (device_id, name)
);
CREATE INDEX virtual_portfolios_device_idx ON virtual_portfolios (device_id);

CREATE TABLE virtual_trades (
    id            BIGSERIAL PRIMARY KEY,
    portfolio_id  UUID NOT NULL REFERENCES virtual_portfolios(id) ON DELETE CASCADE,
    side          TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    coin_id       TEXT NOT NULL,
    amount        DOUBLE PRECISION NOT NULL CHECK (amount > 0),
    price         DOUBLE PRECISION NOT NULL CHECK (price > 0),
    executed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX virtual_trades_portfolio_idx ON virtual_trades (portfolio_id);
CREATE INDEX virtual_trades_executed_at_idx ON virtual_trades (portfolio_id, executed_at DESC);
