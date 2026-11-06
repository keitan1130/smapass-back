BEGIN;
DROP TABLE device_status, device_specs, log_anchors;
DROP TABLE device_logs;
DROP FUNCTION reject_evidence_change(), guard_owned_log(), touch_verified_log();
DROP TABLE anchors, device_keys;
DROP FUNCTION protect_device_key_history();
DROP TABLE key_registration_challenges, app_tokens, app_installations;
DROP TABLE device_ownerships, devices;
COMMIT;
