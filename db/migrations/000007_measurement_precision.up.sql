BEGIN;
-- Preserve the submitted IEEE-754 binary64 thermal observation in its projection.
-- Signed payloads and existing observations are never reconstructed or updated.
ALTER TABLE device_status ALTER COLUMN thermal_headroom TYPE DOUBLE PRECISION;
COMMIT;
