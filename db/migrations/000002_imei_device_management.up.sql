BEGIN;
-- Fill this mapping with verified full IMEIs before migrating existing non-demo devices.
CREATE TABLE IF NOT EXISTS device_imei_migration_map (
    legacy_device_id UUID PRIMARY KEY,
    imei TEXT NOT NULL UNIQUE CHECK (imei ~ '^[0-9]{15}$')
);
-- Only this exact, explicitly labelled demo is automatically mapped.
INSERT INTO device_imei_migration_map (legacy_device_id, imei)
SELECT d.id, '000000000094821' FROM devices d
WHERE d.id = 'd9aaf2bc-6c41-51ed-b18a-2f567dd6075a'
  AND EXISTS (SELECT 1 FROM certificates c WHERE c.device_id = d.id AND c.is_demo)
  AND NOT EXISTS (SELECT 1 FROM certificates c WHERE c.device_id = d.id AND NOT c.is_demo)
  AND EXISTS (SELECT 1 FROM device_keys k WHERE k.device_id = d.id AND k.public_key = 'DEMO-NOT-A-PUBLIC-KEY')
ON CONFLICT DO NOTHING;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM devices d LEFT JOIN device_imei_migration_map m ON m.legacy_device_id = d.id WHERE m.imei IS NULL) THEN
        RAISE EXCEPTION 'full IMEI mapping required; see db/prepare-imei-migration.sql';
    END IF;
END $$;
CREATE TABLE device_legacy_ids AS
SELECT m.imei, d.id AS legacy_device_id FROM devices d JOIN device_imei_migration_map m ON m.legacy_device_id = d.id;
ALTER TABLE device_legacy_ids ADD PRIMARY KEY (imei);
ALTER TABLE device_legacy_ids ADD UNIQUE (legacy_device_id);
ALTER TABLE devices ADD COLUMN imei TEXT;
UPDATE devices d SET imei = m.imei FROM device_imei_migration_map m WHERE m.legacy_device_id = d.id;
ALTER TABLE devices ALTER COLUMN imei SET NOT NULL;
ALTER TABLE devices ADD CHECK (imei ~ '^[0-9]{15}$');
ALTER TABLE device_logs DISABLE TRIGGER immutable_device_logs;
ALTER TABLE device_keys ADD COLUMN imei TEXT;
UPDATE device_keys t SET imei = d.imei FROM devices d WHERE t.device_id = d.id;
ALTER TABLE device_keys ALTER COLUMN imei SET NOT NULL;
ALTER TABLE device_logs ADD COLUMN imei TEXT;
UPDATE device_logs t SET imei = d.imei FROM devices d WHERE t.device_id = d.id;
ALTER TABLE device_logs ALTER COLUMN imei SET NOT NULL;
ALTER TABLE device_specs ADD COLUMN imei TEXT;
UPDATE device_specs t SET imei = d.imei FROM devices d WHERE t.device_id = d.id;
ALTER TABLE device_specs ALTER COLUMN imei SET NOT NULL;
ALTER TABLE device_status ADD COLUMN imei TEXT;
UPDATE device_status t SET imei = d.imei FROM devices d WHERE t.device_id = d.id;
ALTER TABLE device_status ALTER COLUMN imei SET NOT NULL;
ALTER TABLE certificates ADD COLUMN imei TEXT;
UPDATE certificates t SET imei = d.imei FROM devices d WHERE t.device_id = d.id;
ALTER TABLE certificates ALTER COLUMN imei SET NOT NULL;
ALTER TABLE certificate_logs ADD COLUMN imei TEXT;
UPDATE certificate_logs t SET imei = d.imei FROM devices d WHERE t.device_id = d.id;
ALTER TABLE certificate_logs ALTER COLUMN imei SET NOT NULL;
ALTER TABLE device_keys DROP COLUMN device_id CASCADE;
ALTER TABLE device_logs DROP COLUMN device_id CASCADE;
ALTER TABLE device_specs DROP COLUMN device_id CASCADE;
ALTER TABLE device_status DROP COLUMN device_id CASCADE;
ALTER TABLE certificates DROP COLUMN device_id CASCADE;
ALTER TABLE certificate_logs DROP COLUMN device_id CASCADE;
ALTER TABLE devices DROP COLUMN id CASCADE;
ALTER TABLE devices ADD PRIMARY KEY (imei);
ALTER TABLE device_legacy_ids ADD FOREIGN KEY (imei) REFERENCES devices(imei);
UPDATE devices SET imei_suffix = right(imei, 4);
ALTER TABLE device_keys ADD FOREIGN KEY (imei) REFERENCES devices(imei);
ALTER TABLE device_logs ADD FOREIGN KEY (imei) REFERENCES devices(imei);
ALTER TABLE device_specs ADD FOREIGN KEY (imei) REFERENCES devices(imei);
ALTER TABLE device_status ADD FOREIGN KEY (imei) REFERENCES devices(imei);
ALTER TABLE certificates ADD FOREIGN KEY (imei) REFERENCES devices(imei);
ALTER TABLE device_keys ADD UNIQUE (imei, id);
ALTER TABLE device_logs ADD UNIQUE (imei, seq);
ALTER TABLE device_logs ADD UNIQUE (imei, id);
ALTER TABLE device_logs ADD FOREIGN KEY (imei, key_id) REFERENCES device_keys(imei, id);
ALTER TABLE device_specs ADD FOREIGN KEY (imei, log_id) REFERENCES device_logs(imei, id);
ALTER TABLE device_status ADD FOREIGN KEY (imei, log_id) REFERENCES device_logs(imei, id);
ALTER TABLE certificates ADD UNIQUE (imei, id);
ALTER TABLE certificate_logs ADD FOREIGN KEY (imei, certificate_id) REFERENCES certificates(imei, id);
ALTER TABLE certificate_logs ADD FOREIGN KEY (imei, log_id) REFERENCES device_logs(imei, id);
CREATE UNIQUE INDEX idx_device_keys_active ON device_keys(imei) WHERE revoked_at IS NULL;
CREATE INDEX idx_logs_device_time ON device_logs(imei, collected_at, seq);
CREATE INDEX idx_specs_device_time ON device_specs(imei, collected_at DESC);
CREATE INDEX idx_status_device_time ON device_status(imei, collected_at DESC);
CREATE INDEX idx_certificates_device ON certificates(imei, issued_at DESC);
CREATE OR REPLACE FUNCTION protect_certificate_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.imei, NEW.number, NEW.issued_at, NEW.is_demo, NEW.snapshot)
       IS DISTINCT FROM ROW(OLD.imei, OLD.number, OLD.issued_at, OLD.is_demo, OLD.snapshot) THEN
        RAISE EXCEPTION 'issue a new certificate to change snapshot';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TABLE device_imei_aliases (
    imei TEXT PRIMARY KEY CHECK (imei ~ '^[0-9]{15}$'),
    primary_imei TEXT NOT NULL REFERENCES devices(imei)
);
INSERT INTO device_imei_aliases (imei, primary_imei) SELECT imei, imei FROM devices;
CREATE INDEX idx_imei_aliases_device ON device_imei_aliases(primary_imei);
CREATE TABLE device_ownerships (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    imei TEXT NOT NULL REFERENCES devices(imei),
    owner_issuer TEXT NOT NULL,
    owner_subject TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    last_verified_log_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    UNIQUE (imei, id),
    CHECK (ended_at IS NULL OR ended_at >= started_at)
);
CREATE UNIQUE INDEX idx_ownership_active ON device_ownerships(imei) WHERE ended_at IS NULL;
CREATE INDEX idx_ownership_owner ON device_ownerships(owner_issuer, owner_subject) WHERE ended_at IS NULL;
-- Existing records are imported without inventing an authenticated owner or verified logs.
INSERT INTO device_ownerships (imei, owner_issuer, owner_subject, started_at)
SELECT d.imei, 'legacy-import', 'unclaimed', LEAST(d.created_at, COALESCE(min(l.collected_at), d.created_at))
FROM devices d LEFT JOIN device_logs l ON l.imei = d.imei GROUP BY d.imei;
ALTER TABLE device_keys ADD COLUMN ownership_id BIGINT;
ALTER TABLE device_logs ADD COLUMN ownership_id BIGINT;
ALTER TABLE certificates ADD COLUMN ownership_id BIGINT;
UPDATE device_keys k SET ownership_id = o.id FROM device_ownerships o WHERE o.imei = k.imei;
UPDATE device_logs l SET ownership_id = o.id FROM device_ownerships o WHERE o.imei = l.imei;
UPDATE certificates c SET ownership_id = o.id FROM device_ownerships o WHERE o.imei = c.imei;
ALTER TABLE device_keys ALTER COLUMN ownership_id SET NOT NULL;
ALTER TABLE device_logs ALTER COLUMN ownership_id SET NOT NULL;
ALTER TABLE certificates ALTER COLUMN ownership_id SET NOT NULL;
ALTER TABLE device_keys ADD FOREIGN KEY (imei, ownership_id) REFERENCES device_ownerships(imei, id);
ALTER TABLE device_keys ADD UNIQUE (id, ownership_id);
ALTER TABLE device_logs ADD FOREIGN KEY (key_id, ownership_id) REFERENCES device_keys(id, ownership_id);
ALTER TABLE certificates ADD FOREIGN KEY (imei, ownership_id) REFERENCES device_ownerships(imei, id);
ALTER TABLE device_logs ADD COLUMN signature_verified_at TIMESTAMPTZ;
CREATE OR REPLACE FUNCTION protect_certificate_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.imei, NEW.ownership_id, NEW.number, NEW.issued_at, NEW.is_demo, NEW.snapshot)
       IS DISTINCT FROM ROW(OLD.imei, OLD.ownership_id, OLD.number, OLD.issued_at, OLD.is_demo, OLD.snapshot) THEN
        RAISE EXCEPTION 'issue a new certificate to change snapshot';
    END IF;
    RETURN NEW;
END;
$$;
ALTER TABLE device_logs ENABLE TRIGGER immutable_device_logs;
CREATE FUNCTION guard_owned_log() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM devices WHERE imei = NEW.imei FOR UPDATE;
    IF NOT EXISTS (
        SELECT 1 FROM device_ownerships o JOIN device_keys k ON k.ownership_id = o.id
        WHERE o.id = NEW.ownership_id AND o.imei = NEW.imei AND o.ended_at IS NULL
          AND k.id = NEW.key_id AND k.revoked_at IS NULL
    ) THEN RAISE EXCEPTION 'inactive device registration'; END IF;
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
COMMIT;
