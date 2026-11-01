BEGIN;
DROP TRIGGER immutable_device_key_history ON device_keys;
DROP FUNCTION protect_device_key_history();
ALTER TABLE device_keys DROP COLUMN imei_attested;
ALTER TABLE device_keys DROP COLUMN attestation_verified_at;
ALTER TABLE device_keys DROP COLUMN registration_challenge_id;
DROP TABLE key_registration_challenges;
COMMIT;
