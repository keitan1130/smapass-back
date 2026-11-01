BEGIN;
CREATE TABLE app_installations (
    id UUID PRIMARY KEY,
    imei TEXT NOT NULL REFERENCES devices(imei),
    ownership_id BIGINT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    revoked_at TIMESTAMPTZ,
    FOREIGN KEY (imei, ownership_id) REFERENCES device_ownerships(imei, id),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);
CREATE TABLE app_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL REFERENCES app_installations(id),
    access_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(access_hash) = 32),
    refresh_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(refresh_hash) = 32),
    access_expires_at TIMESTAMPTZ NOT NULL,
    refresh_expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    rotated_at TIMESTAMPTZ,
    CHECK (access_expires_at > created_at),
    CHECK (refresh_expires_at > access_expires_at)
);
CREATE UNIQUE INDEX idx_app_tokens_current ON app_tokens(installation_id) WHERE rotated_at IS NULL;
COMMENT ON TABLE app_installations IS 'Anonymous app credentials bound to one IMEI registration period; no user account';
COMMENT ON TABLE app_tokens IS 'SHA-256 digests only; rotated refresh tokens retained for replay detection';
COMMIT;
