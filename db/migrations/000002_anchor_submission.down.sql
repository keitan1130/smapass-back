BEGIN;

-- Re-adding the old status check fails while submitting / submitted rows exist,
-- so this down migration never rewrites an in-flight anchor silently.
DROP INDEX idx_anchors_due;

ALTER TABLE anchors
    DROP CONSTRAINT anchors_status_check,
    DROP CONSTRAINT anchors_submitted_check,
    DROP CONSTRAINT anchors_confirmed_check,
    DROP CONSTRAINT anchors_chain_provider_ref_key,
    DROP COLUMN provider_ref,
    DROP COLUMN block_height,
    DROP COLUMN block_time,
    DROP COLUMN submitted_at,
    DROP COLUMN next_attempt_at,
    DROP COLUMN last_error;

ALTER TABLE anchors
    ADD CONSTRAINT anchors_status_check CHECK (status IN ('pending', 'confirmed', 'failed')),
    ADD CONSTRAINT anchors_check CHECK (status <> 'confirmed' OR (tx_id IS NOT NULL AND confirmed_at IS NOT NULL));

COMMIT;
