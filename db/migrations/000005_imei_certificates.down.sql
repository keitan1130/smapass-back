BEGIN;
ALTER TABLE certificates ADD COLUMN number TEXT;
ALTER TABLE certificates ADD COLUMN successor_number TEXT;
UPDATE certificates c SET number = legacy.number FROM certificate_legacy_numbers legacy
WHERE c.id = legacy.certificate_id;

-- Certificates created after migration have no legacy number. Assign one only
-- when explicitly rolling back to the old application contract.
DO $$
DECLARE entry RECORD; candidate TEXT;
BEGIN
    FOR entry IN SELECT id FROM certificates WHERE number IS NULL LOOP
        LOOP
            candidate := upper(substr(replace(gen_random_uuid()::text, '-', ''), 1, 10));
            EXIT WHEN NOT EXISTS (SELECT 1 FROM certificates WHERE number = candidate);
        END LOOP;
        UPDATE certificates SET number = candidate WHERE id = entry.id;
    END LOOP;
END;
$$;
UPDATE certificates c SET successor_number = successor.number
FROM certificates successor WHERE successor.id = c.successor_id;
ALTER TABLE certificates ALTER COLUMN number SET NOT NULL;
ALTER TABLE certificates ADD UNIQUE (number);
ALTER TABLE certificates ADD CHECK (number ~ '^[0-9A-HJKMNP-TV-Z]{10}$');
ALTER TABLE certificates ADD FOREIGN KEY (successor_number) REFERENCES certificates(number);
ALTER TABLE certificates ADD CHECK ((status = 'superseded') = (successor_number IS NOT NULL));
ALTER TABLE certificates ADD CHECK (successor_number IS NULL OR successor_number <> number);
ALTER TABLE certificates DROP COLUMN successor_id;
CREATE OR REPLACE FUNCTION protect_certificate_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.imei, NEW.ownership_id, NEW.number, NEW.issued_at, NEW.is_demo, NEW.snapshot)
       IS DISTINCT FROM ROW(OLD.imei, OLD.ownership_id, OLD.number, OLD.issued_at, OLD.is_demo, OLD.snapshot) THEN
        RAISE EXCEPTION 'issue a new certificate to change snapshot';
    END IF;
    RETURN NEW;
END;
$$;
DROP TABLE certificate_legacy_numbers;
COMMIT;
