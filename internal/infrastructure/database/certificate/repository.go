package certificate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/internal/infrastructure/database/dto"
)

type Repository struct{ db *bun.DB }

var _ repository.CertificateRepository = (*Repository)(nil)

func NewRepository(db *bun.DB) *Repository { return &Repository{db: db} }

func (r *Repository) selectCertificates(model any) *bun.SelectQuery {
	return r.db.NewSelect().Model(model).
		Column("c.number", "c.status", "c.successor_number", "c.issued_at", "c.is_demo", "c.snapshot").
		ColumnExpr(`CASE WHEN l.certificate_id IS NULL THEN NULL ELSE jsonb_build_object(
			'marketplace', l.marketplace, 'item_id', l.item_id,
			'imei_suffix', l.imei_suffix, 'registered_at', l.registered_at) END AS listing`).
		Join("LEFT JOIN certificate_listings AS l ON l.certificate_id = c.id")
}

func (r *Repository) FindByNumber(ctx context.Context, number string) (*entity.Certificate, error) {
	var row dto.Certificate
	err := r.selectCertificates(&row).Where("c.number = ?", number).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result := row.Entity()
	return &result, nil
}

func (r *Repository) FindByNumbers(ctx context.Context, numbers []string) ([]entity.Certificate, error) {
	rows := make([]dto.Certificate, 0)
	if err := r.selectCertificates(&rows).Where("c.number IN (?)", bun.In(numbers)).OrderExpr("c.number ASC").Scan(ctx); err != nil {
		return nil, err
	}
	result := make([]entity.Certificate, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.Entity())
	}
	return result, nil
}

func (r *Repository) Records(ctx context.Context, number string, limit, offset int) ([]entity.VerificationRecord, int, error) {
	var total int
	err := r.db.NewRaw(`SELECT count(*) FROM certificate_logs cl
		JOIN certificates c ON c.id = cl.certificate_id WHERE c.number = ?`, number).Scan(ctx, &total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT l.seq, l.type, l.collected_at, l.canonical_payload,
		encode(l.payload_hash, 'hex'), encode(l.prev_hash, 'hex'), encode(l.signature, 'base64'),
		k.public_key, k.signature_algorithm, l.canonicalization,
		COALESCE(a.status, 'unanchored'), a.chain, a.tx_id, encode(a.merkle_root, 'hex'),
		la.leaf_index, COALESCE(la.merkle_proof, '[]'::jsonb)
		FROM certificate_logs cl
		JOIN certificates c ON c.id = cl.certificate_id
		JOIN device_logs l ON l.id = cl.log_id
		JOIN device_keys k ON k.id = l.key_id
		LEFT JOIN log_anchors la ON la.log_id = l.id
		LEFT JOIN anchors a ON a.id = la.anchor_id
		WHERE c.number = ? ORDER BY l.seq ASC LIMIT ? OFFSET ?`, number, limit, offset)
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
