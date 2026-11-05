BEGIN;

-- Retain old numbers privately only for rollback; no application query uses them.
CREATE TABLE certificate_legacy_numbers (
    certificate_id UUID PRIMARY KEY REFERENCES certificates(id),
    number TEXT NOT NULL UNIQUE
);
INSERT INTO certificate_legacy_numbers SELECT id, number FROM certificates;

ALTER TABLE certificates ADD COLUMN successor_id UUID REFERENCES certificates(id);
UPDATE certificates c SET successor_id = successor.id
FROM certificates successor WHERE successor.number = c.successor_number;

ALTER TABLE certificates DROP COLUMN successor_number;
ALTER TABLE certificates DROP COLUMN number;
ALTER TABLE certificates ADD CONSTRAINT certificates_successor_state
    CHECK ((status = 'superseded') = (successor_id IS NOT NULL));
ALTER TABLE certificates ADD CONSTRAINT certificates_successor_not_self
    CHECK (successor_id IS NULL OR successor_id <> id);

CREATE OR REPLACE FUNCTION protect_certificate_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.id, NEW.imei, NEW.ownership_id, NEW.issued_at, NEW.is_demo, NEW.snapshot)
       IS DISTINCT FROM ROW(OLD.id, OLD.imei, OLD.ownership_id, OLD.issued_at, OLD.is_demo, OLD.snapshot) THEN
        RAISE EXCEPTION 'issue a new certificate to change snapshot';
    END IF;
    RETURN NEW;
END;
$$;

COMMENT ON COLUMN certificates.id IS 'Internal history/log relationship only; public APIs select by IMEI';
COMMENT ON TABLE certificate_legacy_numbers IS 'Private rollback archive, not a public certificate identifier';
COMMIT;
