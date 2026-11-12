BEGIN;

-- Separate "not yet sent" from "sent and waiting for a block" so a crash between
-- the external request and the database update can be detected and reconciled.
ALTER TABLE anchors
    DROP CONSTRAINT anchors_status_check,
    DROP CONSTRAINT anchors_check;

ALTER TABLE anchors
    ADD COLUMN provider_ref TEXT,
    ADD COLUMN block_height BIGINT CHECK (block_height >= 0),
    ADD COLUMN block_time TIMESTAMPTZ,
    ADD COLUMN submitted_at TIMESTAMPTZ,
    ADD COLUMN next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN last_error TEXT,
    ADD CONSTRAINT anchors_status_check
        CHECK (status IN ('pending', 'submitting', 'submitted', 'confirmed', 'failed')),
    ADD CONSTRAINT anchors_submitted_check
        CHECK (status NOT IN ('submitted', 'confirmed') OR (provider_ref IS NOT NULL AND submitted_at IS NOT NULL)),
    ADD CONSTRAINT anchors_confirmed_check
        CHECK (status <> 'confirmed' OR (tx_id IS NOT NULL AND confirmed_at IS NOT NULL
            AND block_height IS NOT NULL AND block_time IS NOT NULL)),
    ADD CONSTRAINT anchors_chain_provider_ref_key UNIQUE (chain, provider_ref);

CREATE INDEX idx_anchors_due ON anchors(next_attempt_at)
    WHERE status IN ('pending', 'submitting', 'submitted');

COMMIT;
