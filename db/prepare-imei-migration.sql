-- Run before migration if the old UUID-based DB contains non-demo devices.
-- Fill each legacy UUID with a verified full IMEI; never derive it from a suffix.
CREATE TABLE IF NOT EXISTS device_imei_migration_map (
    legacy_device_id UUID PRIMARY KEY,
    imei TEXT NOT NULL UNIQUE CHECK (imei ~ '^[0-9]{15}$')
);
-- INSERT INTO device_imei_migration_map (legacy_device_id, imei) VALUES ('existing UUID', 'verified 15-digit IMEI');
