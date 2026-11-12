package device

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
)

func (r *Repository) Records(ctx context.Context, id string, limit, offset int) ([]entity.VerificationRecord, int, error) {
	var items []entity.VerificationRecord
	var total int
	err := r.db.RunInTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead}, func(ctx context.Context, tx bun.Tx) error {
		var err error
		items, total, err = readRecords(ctx, tx, id, limit, offset)
		return err
	})
	return items, total, err
}
func readRecords(ctx context.Context, tx bun.IDB, id string, limit, offset int) ([]entity.VerificationRecord, int, error) {
	deviceID, err := requireDeviceID(ctx, tx, id)
	if err != nil {
		return nil, 0, err
	}
	var total int
	err = tx.NewRaw(`SELECT count(*) FROM device_logs l WHERE l.device_id=?`, deviceID).Scan(ctx, &total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT l.seq, l.type, l.collected_at, l.canonical_payload,
		encode(l.payload_hash, 'hex'), encode(l.prev_hash, 'hex'), encode(l.signature, 'base64'),
		k.public_key, k.signature_algorithm, l.canonicalization,
		CASE WHEN a.status IS NULL THEN 'unanchored'
			WHEN a.status IN ('submitting', 'submitted') THEN 'pending'
			ELSE a.status END,
		a.chain, a.tx_id, encode(a.merkle_root, 'hex'),
		la.leaf_index, COALESCE(la.merkle_proof, '[]'::jsonb)
		FROM device_logs l
		JOIN device_keys k ON k.id = l.key_id
		LEFT JOIN log_anchors la ON la.log_id = l.id
		LEFT JOIN anchors a ON a.id = la.anchor_id
		WHERE l.device_id = ? ORDER BY l.seq ASC LIMIT ? OFFSET ?`, deviceID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]entity.VerificationRecord, 0)
	for rows.Next() {
		var record entity.VerificationRecord
		var proof []byte
		err = rows.Scan(&record.Sequence, &record.Type, &record.CollectedAt, &record.CanonicalPayload,
			&record.PayloadHash, &record.PreviousHash, &record.SignatureBase64, &record.PublicKey,
			&record.SignatureAlgorithm, &record.Canonicalization, &record.AnchorState, &record.Chain,
			&record.TransactionHash, &record.MerkleRoot, &record.LeafIndex, &proof)
		if err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal(proof, &record.MerkleProof); err != nil {
			return nil, 0, err
		}
		result = append(result, record)
	}
	return result, total, rows.Err()
}
