BEGIN;
-- Explicit rollback narrows display precision; signed original bytes remain intact.
ALTER TABLE device_status ALTER COLUMN thermal_headroom TYPE REAL;
COMMIT;
