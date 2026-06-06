CREATE TABLE devices (
    device_id   UUID PRIMARY KEY,
    apns_token  TEXT NOT NULL,
    apns_env    TEXT NOT NULL CHECK (apns_env IN ('development','production')),
    locale      TEXT NOT NULL DEFAULT 'en',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE alerts (
    id                     UUID PRIMARY KEY,
    device_id              UUID NOT NULL REFERENCES devices(device_id) ON DELETE CASCADE,
    condition_json         JSONB NOT NULL,
    recurrence_json        JSONB NOT NULL,
    is_active              BOOLEAN NOT NULL DEFAULT true,
    fired_at               TIMESTAMPTZ,
    last_condition_result  BOOLEAN,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX alerts_active_idx ON alerts (is_active) WHERE is_active = true;
CREATE INDEX alerts_device_idx ON alerts (device_id);

CREATE TABLE holdings (
    device_id          UUID NOT NULL REFERENCES devices(device_id) ON DELETE CASCADE,
    coin_id            TEXT NOT NULL,
    amount             DOUBLE PRECISION NOT NULL,
    average_buy_price  DOUBLE PRECISION NOT NULL,
    date_added         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (device_id, coin_id)
);
CREATE INDEX holdings_device_idx ON holdings (device_id);

CREATE TABLE firings (
    id               BIGSERIAL PRIMARY KEY,
    alert_id         UUID NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    device_id        UUID NOT NULL REFERENCES devices(device_id) ON DELETE CASCADE,
    fired_at         TIMESTAMPTZ NOT NULL,
    actual_value     DOUBLE PRECISION,
    delivery_status  TEXT NOT NULL CHECK (delivery_status IN ('pending','delivered','failed','dropped'))
);
CREATE INDEX firings_alert_idx ON firings (alert_id);
