BEGIN;

-- Live certificates are projections of append-only device logs. No issuance tables.

-- Device identity and registration periods.
CREATE TABLE devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    imei TEXT NOT NULL UNIQUE CHECK (imei ~ '^[0-9]{15}$'),
    visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('public','private')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE device_ownerships (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    device_id UUID NOT NULL REFERENCES devices(id),
    owner_issuer TEXT NOT NULL,
    owner_subject TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    last_verified_log_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    UNIQUE (device_id, id),
    CHECK (ended_at IS NULL OR ended_at >= started_at)
);

CREATE UNIQUE INDEX idx_ownership_active ON device_ownerships(device_id) WHERE ended_at IS NULL;
CREATE INDEX idx_ownership_owner ON device_ownerships(owner_issuer,owner_subject) WHERE ended_at IS NULL;

-- Anonymous application credentials and replay detection.
CREATE TABLE app_installations (
    id UUID PRIMARY KEY,
    device_id UUID NOT NULL REFERENCES devices(id),
    ownership_id BIGINT NOT NULL UNIQUE,
    UNIQUE (id,device_id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    revoked_at TIMESTAMPTZ,
    FOREIGN KEY (device_id, ownership_id) REFERENCES device_ownerships(device_id, id),
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
COMMENT ON TABLE app_installations IS 'Anonymous app credentials bound to one device registration period; no user account';
COMMENT ON TABLE app_tokens IS 'SHA-256 digests only; rotated refresh tokens retained for replay detection';

-- Hardware key registration and rotation.
CREATE TABLE key_registration_challenges (
    id UUID PRIMARY KEY,
    imei TEXT CHECK (imei ~ '^[0-9]{15}$'),
    device_id UUID REFERENCES devices(id),
    purpose TEXT NOT NULL CHECK (purpose IN ('enroll','rotate')),
    installation_id UUID REFERENCES app_installations(id),
    nonce BYTEA NOT NULL UNIQUE CHECK (octet_length(nonce)=32),
    signing_payload BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    CHECK ((purpose='rotate') = (installation_id IS NOT NULL)),
    CHECK ((purpose='enroll' AND imei IS NOT NULL AND device_id IS NULL) OR
           (purpose='rotate' AND imei IS NULL AND device_id IS NOT NULL)),
    FOREIGN KEY (installation_id,device_id) REFERENCES app_installations(id,device_id),
    CHECK (expires_at > created_at)
);

CREATE INDEX idx_key_challenges_expiry ON key_registration_challenges(expires_at);

CREATE TABLE device_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id),
    public_key TEXT NOT NULL UNIQUE,
    signature_algorithm TEXT NOT NULL,
    attestation JSONB,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    UNIQUE (device_id, id),
    ownership_id BIGINT NOT NULL,
    FOREIGN KEY (device_id,ownership_id) REFERENCES device_ownerships(device_id,id),
    UNIQUE (id,ownership_id),
    registration_challenge_id UUID UNIQUE REFERENCES key_registration_challenges(id),
    attestation_verified_at TIMESTAMPTZ,
    imei_attested BOOLEAN NOT NULL DEFAULT false,
    CHECK ((registration_challenge_id IS NULL) = (attestation_verified_at IS NULL)),
    CHECK (revoked_at IS NULL OR revoked_at >= registered_at)
);

CREATE UNIQUE INDEX idx_device_keys_active ON device_keys(device_id) WHERE revoked_at IS NULL;

-- External verification metadata is separate from immutable signed evidence.
CREATE TABLE anchors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chain TEXT NOT NULL,
    merkle_root BYTEA NOT NULL CHECK (octet_length(merkle_root) = 32),
    leaf_count INTEGER NOT NULL CHECK (leaf_count > 0),
    tx_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'failed')),
    retry_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at TIMESTAMPTZ,
    UNIQUE (chain, tx_id),
    CHECK (status <> 'confirmed' OR (tx_id IS NOT NULL AND confirmed_at IS NOT NULL))
);

-- Canonical signed logs; sequence and previous hash continue across registrants.
CREATE TABLE device_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id),
    key_id UUID NOT NULL,
    ownership_id BIGINT NOT NULL,
    signature_verified_at TIMESTAMPTZ,
    FOREIGN KEY (key_id,ownership_id) REFERENCES device_keys(id,ownership_id),
    seq BIGINT NOT NULL CHECK (seq > 0),
    type TEXT NOT NULL CHECK (type IN ('spec', 'status', 'inspection')),
    canonicalization TEXT NOT NULL,
    canonical_payload TEXT NOT NULL,
    payload JSONB NOT NULL,
    payload_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(payload_hash) = 32),
    prev_hash BYTEA CHECK (octet_length(prev_hash) = 32),
    signature BYTEA NOT NULL CHECK (octet_length(signature) > 0),
    os_proof JSONB,
    collected_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (device_id, seq),
    UNIQUE (device_id, id),
    FOREIGN KEY (device_id, key_id) REFERENCES device_keys(device_id, id),
    CHECK (jsonb_typeof(payload) = 'object'),
    CHECK (canonical_payload::jsonb = payload),
    CHECK (payload_hash = sha256(convert_to(canonical_payload, 'UTF8'))),
    CHECK ((seq = 1 AND prev_hash IS NULL) OR (seq > 1 AND prev_hash IS NOT NULL))
);

CREATE INDEX idx_logs_device_time ON device_logs(device_id,collected_at,seq);

CREATE TABLE log_anchors (
    log_id UUID PRIMARY KEY REFERENCES device_logs(id),
    anchor_id UUID NOT NULL REFERENCES anchors(id),
    leaf_index INTEGER NOT NULL CHECK (leaf_index >= 0),
    merkle_proof JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(merkle_proof) = 'array'),
    UNIQUE (anchor_id, leaf_index)
);

-- Read projections derived from the signed logs.
CREATE TABLE device_specs (
    log_id UUID PRIMARY KEY,
    device_id UUID NOT NULL REFERENCES devices(id),
    manufacturer TEXT NOT NULL,
    model TEXT NOT NULL,
    device_name TEXT,
    soc TEXT,
    cpu_cores SMALLINT CHECK (cpu_cores > 0),
    ram_bytes BIGINT CHECK (ram_bytes > 0),
    storage_bytes BIGINT CHECK (storage_bytes > 0),
    screen_size_inch NUMERIC(4,2) CHECK (screen_size_inch > 0),
    screen_density_dpi SMALLINT CHECK (screen_density_dpi > 0),
    collected_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (device_id, log_id) REFERENCES device_logs(device_id, id)
);

CREATE INDEX idx_specs_device_time ON device_specs(device_id,collected_at DESC);

CREATE TABLE device_status (
    log_id UUID PRIMARY KEY,
    device_id UUID NOT NULL REFERENCES devices(id),
    os_version TEXT NOT NULL,
    sdk_int SMALLINT NOT NULL CHECK (sdk_int > 0),
    battery_temp_c NUMERIC(5,1),
    battery_cycle_count INTEGER CHECK (battery_cycle_count >= 0),
    battery_charge_mah INTEGER CHECK (battery_charge_mah >= 0),
    battery_full_charge_mah INTEGER CHECK (battery_full_charge_mah > 0),
    boot_count INTEGER CHECK (boot_count >= 0),
    had_abnormal_exit BOOLEAN,
    thermal_headroom DOUBLE PRECISION,
    collected_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (device_id, log_id) REFERENCES device_logs(device_id, id)
);

CREATE INDEX idx_status_device_time ON device_status(device_id,collected_at DESC);

CREATE FUNCTION reject_evidence_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'signed evidence is append-only';
END;
$$;
CREATE TRIGGER immutable_device_logs BEFORE UPDATE OR DELETE ON device_logs
FOR EACH ROW EXECUTE FUNCTION reject_evidence_change();

COMMENT ON COLUMN device_keys.attestation IS 'Private attestation evidence and verification policy; never expose through public records';
CREATE FUNCTION protect_device_key_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'device key history is append-only'; END IF;
    IF ROW(NEW.id,NEW.device_id,NEW.ownership_id,NEW.public_key,NEW.signature_algorithm,NEW.attestation,NEW.registered_at,NEW.registration_challenge_id,NEW.attestation_verified_at,NEW.imei_attested)
        IS DISTINCT FROM ROW(OLD.id,OLD.device_id,OLD.ownership_id,OLD.public_key,OLD.signature_algorithm,OLD.attestation,OLD.registered_at,OLD.registration_challenge_id,OLD.attestation_verified_at,OLD.imei_attested)
        OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
        RAISE EXCEPTION 'device key history is immutable; rotate the key instead';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER immutable_device_key_history BEFORE UPDATE OR DELETE ON device_keys FOR EACH ROW EXECUTE FUNCTION protect_device_key_history();

CREATE FUNCTION guard_owned_log() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous RECORD;
BEGIN
    PERFORM 1 FROM devices WHERE id = NEW.device_id FOR UPDATE;
    IF NOT EXISTS (
        SELECT 1 FROM device_ownerships o JOIN device_keys k ON k.ownership_id = o.id
        WHERE o.id = NEW.ownership_id AND o.device_id = NEW.device_id AND o.ended_at IS NULL
          AND k.id = NEW.key_id AND k.revoked_at IS NULL
    ) THEN RAISE EXCEPTION 'inactive device registration'; END IF;
    SELECT seq, payload_hash INTO previous FROM device_logs WHERE device_id=NEW.device_id ORDER BY seq DESC LIMIT 1;
    IF NEW.seq <> COALESCE(previous.seq,0)+1 OR NEW.prev_hash IS DISTINCT FROM previous.payload_hash THEN
        RAISE EXCEPTION 'log must continue the device sequence and previous hash';
    END IF;
    NEW.received_at := clock_timestamp();
    IF NEW.signature_verified_at IS NOT NULL THEN NEW.signature_verified_at := NEW.received_at; END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER owned_log_guard BEFORE INSERT ON device_logs FOR EACH ROW EXECUTE FUNCTION guard_owned_log();

CREATE FUNCTION touch_verified_log() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.signature_verified_at IS NOT NULL THEN
        UPDATE device_ownerships SET last_verified_log_at = GREATEST(last_verified_log_at, NEW.received_at)
        WHERE id = NEW.ownership_id;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER verified_log_activity AFTER INSERT ON device_logs FOR EACH ROW EXECUTE FUNCTION touch_verified_log();

-- One public-read policy for search, summaries, state, history and evidence.
-- No scheduled job is needed at the exact 72-hour boundary.
CREATE VIEW public_devices AS
SELECT d.* FROM devices d
WHERE d.visibility='public' OR NOT EXISTS (
    SELECT 1 FROM device_ownerships o WHERE o.device_id=d.id AND o.ended_at IS NULL
      AND COALESCE(o.last_verified_log_at,o.started_at) + interval '72 hours' > transaction_timestamp()
);

COMMIT;
