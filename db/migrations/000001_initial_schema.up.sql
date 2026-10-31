BEGIN;

CREATE TABLE shops (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    public_key TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Store only the suffix required for public listing matching in this scaffold.
CREATE TABLE devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    imei_suffix TEXT CHECK (imei_suffix ~ '^[0-9]{4}$'),
    registered_by UUID REFERENCES shops(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE device_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id),
    public_key TEXT NOT NULL UNIQUE,
    signature_algorithm TEXT NOT NULL,
    attestation JSONB,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    UNIQUE (device_id, id),
    CHECK (revoked_at IS NULL OR revoked_at >= registered_at)
);
CREATE UNIQUE INDEX idx_device_keys_active ON device_keys(device_id) WHERE revoked_at IS NULL;

-- Chain adapter, RPC submission and confirmation worker are not implemented yet.
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

CREATE TABLE device_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id),
    key_id UUID NOT NULL,
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
CREATE INDEX idx_logs_device_time ON device_logs(device_id, collected_at, seq);

-- Keep mutable anchoring metadata outside immutable evidence.
CREATE TABLE log_anchors (
    log_id UUID PRIMARY KEY REFERENCES device_logs(id),
    anchor_id UUID NOT NULL REFERENCES anchors(id),
    leaf_index INTEGER NOT NULL CHECK (leaf_index >= 0),
    merkle_proof JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(merkle_proof) = 'array'),
    UNIQUE (anchor_id, leaf_index)
);

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
CREATE INDEX idx_specs_device_time ON device_specs(device_id, collected_at DESC);

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
    thermal_headroom REAL,
    collected_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (device_id, log_id) REFERENCES device_logs(device_id, id)
);
CREATE INDEX idx_status_device_time ON device_status(device_id, collected_at DESC);

CREATE TABLE certificates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id),
    number TEXT NOT NULL UNIQUE CHECK (number ~ '^[0-9A-HJKMNP-TV-Z]{10}$'),
    status TEXT NOT NULL DEFAULT 'valid' CHECK (status IN ('valid', 'revoked', 'superseded')),
    successor_number TEXT REFERENCES certificates(number),
    issued_at TIMESTAMPTZ NOT NULL,
    is_demo BOOLEAN NOT NULL DEFAULT FALSE,
    snapshot JSONB NOT NULL CHECK (jsonb_typeof(snapshot) = 'object'),
    UNIQUE (device_id, id),
    CHECK ((status = 'superseded') = (successor_number IS NOT NULL)),
    CHECK (successor_number IS NULL OR successor_number <> number)
);
CREATE INDEX idx_certificates_device ON certificates(device_id, issued_at DESC);

CREATE TABLE certificate_logs (
    certificate_id UUID NOT NULL,
    device_id UUID NOT NULL,
    log_id UUID NOT NULL,
    PRIMARY KEY (certificate_id, log_id),
    FOREIGN KEY (device_id, certificate_id) REFERENCES certificates(device_id, id),
    FOREIGN KEY (device_id, log_id) REFERENCES device_logs(device_id, id)
);

CREATE TABLE certificate_listings (
    certificate_id UUID PRIMARY KEY REFERENCES certificates(id),
    marketplace TEXT NOT NULL,
    item_id TEXT NOT NULL,
    imei_suffix TEXT CHECK (imei_suffix ~ '^[0-9]{4}$'),
    registered_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE FUNCTION reject_evidence_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'signed evidence is append-only';
END;
$$;
CREATE TRIGGER immutable_device_logs BEFORE UPDATE OR DELETE ON device_logs
FOR EACH ROW EXECUTE FUNCTION reject_evidence_change();

CREATE FUNCTION protect_certificate_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.device_id, NEW.number, NEW.issued_at, NEW.is_demo, NEW.snapshot)
       IS DISTINCT FROM ROW(OLD.device_id, OLD.number, OLD.issued_at, OLD.is_demo, OLD.snapshot) THEN
        RAISE EXCEPTION 'issue a new certificate to change snapshot';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER immutable_certificate_snapshot BEFORE UPDATE ON certificates
FOR EACH ROW EXECUTE FUNCTION protect_certificate_snapshot();

COMMIT;
