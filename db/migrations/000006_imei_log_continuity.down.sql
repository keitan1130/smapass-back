BEGIN;
CREATE OR REPLACE FUNCTION guard_owned_log() RETURNS trigger LANGUAGE plpgsql AS $$
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
COMMIT;
