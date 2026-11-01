BEGIN;
CREATE TABLE key_registration_challenges (
    id UUID PRIMARY KEY,
    imei TEXT NOT NULL CHECK (imei ~ '^[0-9]{15}$'),
    imeis JSONB NOT NULL CHECK (jsonb_typeof(imeis) = 'array'),
    purpose TEXT NOT NULL CHECK (purpose IN ('enroll','rotate')),
    installation_id UUID REFERENCES app_installations(id),
    nonce BYTEA NOT NULL UNIQUE CHECK (octet_length(nonce)=32),
    signing_payload BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    CHECK ((purpose='rotate') = (installation_id IS NOT NULL)),
    CHECK (expires_at > created_at)
);
CREATE INDEX idx_key_challenges_expiry ON key_registration_challenges(expires_at);
ALTER TABLE device_keys ADD COLUMN registration_challenge_id UUID UNIQUE REFERENCES key_registration_challenges(id);
ALTER TABLE device_keys ADD COLUMN attestation_verified_at TIMESTAMPTZ;
ALTER TABLE device_keys ADD COLUMN imei_attested BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE device_keys ADD CHECK ((registration_challenge_id IS NULL) = (attestation_verified_at IS NULL));
COMMENT ON COLUMN device_keys.attestation IS 'Private attestation evidence and verification policy; never expose through public records';
CREATE FUNCTION protect_device_key_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'device key history is append-only'; END IF;
    IF ROW(NEW.id,NEW.imei,NEW.ownership_id,NEW.public_key,NEW.signature_algorithm,NEW.attestation,NEW.registered_at,NEW.registration_challenge_id,NEW.attestation_verified_at,NEW.imei_attested)
        IS DISTINCT FROM ROW(OLD.id,OLD.imei,OLD.ownership_id,OLD.public_key,OLD.signature_algorithm,OLD.attestation,OLD.registered_at,OLD.registration_challenge_id,OLD.attestation_verified_at,OLD.imei_attested)
        OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
        RAISE EXCEPTION 'device key history is immutable; rotate the key instead';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER immutable_device_key_history BEFORE UPDATE OR DELETE ON device_keys FOR EACH ROW EXECUTE FUNCTION protect_device_key_history();
COMMIT;
