package anchor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
)

// lastErrorLimit keeps provider messages short; they must never contain credentials.
const lastErrorLimit = 500

type Repository struct{ db *bun.DB }

func NewRepository(db *bun.DB) *Repository { return &Repository{db: db} }

var _ repository.AnchorRepository = (*Repository)(nil)

var errStateChanged = errors.New("anchor state changed concurrently")

func (r *Repository) CreateBatch(ctx context.Context, chain string, limit int, build repository.MerkleBuilder) (*entity.Anchor, error) {
	var created *entity.Anchor
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// One batcher at a time, so two workers never pick the same logs.
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext('smapass.anchor.batch'))"); err != nil {
			return err
		}
		// Only logs whose signature was verified at ingestion; demo / unverified logs are never anchored.
		rows, err := tx.QueryContext(ctx, `SELECT l.id, l.payload_hash FROM device_logs l
			WHERE l.signature_verified_at IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM log_anchors la WHERE la.log_id = l.id)
			ORDER BY l.received_at, l.id LIMIT ?`, limit)
		if err != nil {
			return err
		}
		var ids []string
		var hashes [][32]byte
		for rows.Next() {
			var id string
			var hash []byte
			if err := rows.Scan(&id, &hash); err != nil {
				_ = rows.Close()
				return err
			}
			if len(hash) != 32 {
				_ = rows.Close()
				return fmt.Errorf("payload hash of log %s is not 32 bytes", id)
			}
			ids, hashes = append(ids, id), append(hashes, [32]byte(hash))
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil || len(ids) == 0 {
			return err
		}
		root, proofs, err := build(hashes)
		if err != nil {
			return err
		}
		anchor := entity.Anchor{Chain: chain, MerkleRoot: root, LeafCount: len(ids), Status: entity.AnchorPending}
		if err := tx.NewRaw(`INSERT INTO anchors (chain, merkle_root, leaf_count, status)
			VALUES (?, ?, ?, 'pending') RETURNING id`, chain, root[:], len(ids)).Scan(ctx, &anchor.ID); err != nil {
			return err
		}
		for i, id := range ids {
			proof, err := json.Marshal(proofs[i])
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO log_anchors (log_id, anchor_id, leaf_index, merkle_proof)
				VALUES (?, ?, ?, ?::jsonb)`, id, anchor.ID, i, string(proof)); err != nil {
				return err
			}
		}
		created = &anchor
		return nil
	})
	return created, err
}

func (r *Repository) ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]entity.Anchor, error) {
	rows, err := r.db.QueryContext(ctx, `UPDATE anchors a SET next_attempt_at = now() + make_interval(secs => ?)
		FROM (SELECT id FROM anchors
			WHERE status IN ('pending', 'submitting', 'submitted') AND next_attempt_at <= now()
			ORDER BY next_attempt_at, created_at LIMIT ? FOR UPDATE SKIP LOCKED) due
		WHERE a.id = due.id
		RETURNING a.id, a.chain, a.merkle_root, a.leaf_count, a.status, a.provider_ref, a.retry_count`,
		lease.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var anchors []entity.Anchor
	for rows.Next() {
		var a entity.Anchor
		var root []byte
		if err := rows.Scan(&a.ID, &a.Chain, &root, &a.LeafCount, &a.Status, &a.ProviderRef, &a.RetryCount); err != nil {
			return nil, err
		}
		a.MerkleRoot = [32]byte(root)
		anchors = append(anchors, a)
	}
	return anchors, rows.Err()
}

func (r *Repository) MarkSubmitting(ctx context.Context, id string) error {
	return r.update(ctx, `UPDATE anchors SET status = 'submitting'
		WHERE id = ? AND status IN ('pending', 'submitting')`, id)
}

func (r *Repository) MarkSubmitted(ctx context.Context, id string, receipt entity.TimestampReceipt, nextPoll time.Duration) error {
	return r.update(ctx, `UPDATE anchors SET status = 'submitted', provider_ref = ?, tx_id = ?,
		submitted_at = COALESCE(submitted_at, now()), next_attempt_at = now() + make_interval(secs => ?), last_error = NULL
		WHERE id = ? AND status IN ('submitting', 'submitted')`,
		receipt.Ref, receipt.TxID, nextPoll.Seconds(), id)
}

func (r *Repository) MarkConfirmed(ctx context.Context, id string, receipt entity.TimestampReceipt) error {
	if !receipt.Confirmed() {
		return fmt.Errorf("receipt is not confirmed")
	}
	return r.update(ctx, `UPDATE anchors SET status = 'confirmed', provider_ref = ?, tx_id = ?,
		block_height = ?, block_time = ?, submitted_at = COALESCE(submitted_at, now()),
		confirmed_at = now(), last_error = NULL
		WHERE id = ? AND status IN ('submitting', 'submitted')`,
		receipt.Ref, *receipt.TxID, *receipt.BlockHeight, receipt.BlockTime.UTC(), id)
}

func (r *Repository) Reschedule(ctx context.Context, id string, delay time.Duration, reason string) error {
	return r.update(ctx, `UPDATE anchors SET retry_count = retry_count + 1,
		next_attempt_at = now() + make_interval(secs => ?), last_error = ?
		WHERE id = ? AND status IN ('pending', 'submitting', 'submitted')`,
		delay.Seconds(), truncate(reason), id)
}

func (r *Repository) MarkFailed(ctx context.Context, id string, reason string) error {
	return r.update(ctx, `UPDATE anchors SET status = 'failed', last_error = ?
		WHERE id = ? AND status IN ('pending', 'submitting', 'submitted')`, truncate(reason), id)
}

func (r *Repository) update(ctx context.Context, query string, args ...any) error {
	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errStateChanged
	}
	return nil
}

// truncate cuts on a rune boundary so PostgreSQL never receives invalid UTF-8.
func truncate(s string) string {
	if len(s) <= lastErrorLimit {
		return s
	}
	cut := lastErrorLimit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
