package certificate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/internal/infrastructure/database/authorization"
	"smapass-back/internal/infrastructure/database/dto"
)

type Repository struct{ db *bun.DB }

var _ repository.CertificateRepository = (*Repository)(nil)

func NewRepository(db *bun.DB) *Repository { return &Repository{db: db} }

func (r *Repository) selectCertificates(model any) *bun.SelectQuery {
	return r.db.NewSelect().Model(model).
		Column("c.id", "c.imei", "c.status", "c.issued_at", "c.is_demo", "c.snapshot").
		ColumnExpr(`CASE WHEN l.certificate_id IS NULL OR o.ended_at IS NOT NULL OR c.status <> 'valid' THEN NULL ELSE jsonb_build_object(
			'marketplace', l.marketplace, 'item_id', l.item_id,
			'imei_suffix', l.imei_suffix, 'registered_at', l.registered_at) END AS listing`).
		Join("JOIN device_ownerships o ON o.id=c.ownership_id").
		Join("LEFT JOIN certificate_listings AS l ON l.certificate_id = c.id")
}

// Issued snapshots belong to the IMEI history. Registration expiry or
// revocation changes writing authority, not public access to historical state.
const latestCertificates = `SELECT DISTINCT ON (c.imei) c.id, c.imei
    FROM certificates c ORDER BY c.imei, c.issued_at DESC, c.id DESC`

func (r *Repository) FindLatestByIMEIs(ctx context.Context, imeis []string) ([]entity.Certificate, error) {
	rows := make([]dto.Certificate, 0)
	err := r.selectCertificates(&rows).Where(`c.id IN (SELECT latest.id FROM (`+latestCertificates+`) latest
        WHERE latest.imei IN
        (SELECT primary_imei FROM device_imei_aliases WHERE imei IN (?)))`, bun.In(imeis)).
		OrderExpr("c.imei ASC").Scan(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]entity.Certificate, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.Entity())
	}
	return result, nil
}

func (r *Repository) Records(ctx context.Context, imei string, limit, offset int) ([]entity.VerificationRecord, int, error) {
	var primary string
	err := r.db.NewRaw("SELECT primary_imei FROM device_imei_aliases WHERE imei=?", imei).Scan(ctx, &primary)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, repository.ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	var total int
	err = r.db.NewRaw(`SELECT count(*) FROM device_logs l WHERE l.imei=? AND EXISTS (SELECT 1 FROM certificate_logs cl WHERE cl.log_id=l.id)`, primary).Scan(ctx, &total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT l.seq, l.type, l.collected_at, l.canonical_payload,
		encode(l.payload_hash, 'hex'), encode(l.prev_hash, 'hex'), encode(l.signature, 'base64'),
		k.public_key, k.signature_algorithm, l.canonicalization,
		COALESCE(a.status, 'unanchored'), a.chain, a.tx_id, encode(a.merkle_root, 'hex'),
		la.leaf_index, COALESCE(la.merkle_proof, '[]'::jsonb)
		FROM device_logs l
		JOIN device_keys k ON k.id = l.key_id
		LEFT JOIN log_anchors la ON la.log_id = l.id
		LEFT JOIN anchors a ON a.id = la.anchor_id
		WHERE l.imei = ? AND EXISTS (SELECT 1 FROM certificate_logs cl WHERE cl.log_id=l.id) ORDER BY l.seq ASC LIMIT ? OFFSET ?`, primary, limit, offset)
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

func (r *Repository) RevokeOwned(ctx context.Context, p entity.Principal, imei string) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var primary string
		err := tx.NewRaw(`SELECT d.imei FROM devices d JOIN device_imei_aliases a ON a.primary_imei=d.imei
            WHERE a.imei=? FOR UPDATE OF d`, imei).Scan(ctx, &primary)
		if errors.Is(err, sql.ErrNoRows) {
			return repository.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := authorization.RequireDevice(ctx, tx, p, primary); err != nil {
			return err
		}
		var latest struct {
			ID     string
			Status string
		}
		err = tx.NewRaw(`SELECT c.id,c.status FROM certificates c
            JOIN device_ownerships o ON o.id=c.ownership_id AND o.imei=c.imei
            WHERE c.imei=? AND o.ended_at IS NULL AND o.owner_issuer=? AND o.owner_subject=?
            ORDER BY c.issued_at DESC,c.id DESC LIMIT 1 FOR UPDATE OF c`, primary, p.Issuer, p.Subject).Scan(ctx, &latest)
		if errors.Is(err, sql.ErrNoRows) {
			return repository.ErrNotFound
		}
		if err != nil {
			return err
		}
		if latest.Status == "superseded" {
			return entity.ErrCertificateState
		}
		_, err = tx.ExecContext(ctx, "UPDATE certificates SET status='revoked' WHERE id=? AND status='valid'", latest.ID)
		return err
	})
}

func (r *Repository) FindLatestByIMEI(ctx context.Context, imei string) (*entity.Certificate, error) {
	items, err := r.FindLatestByIMEIs(ctx, []string{imei})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, repository.ErrNotFound
	}
	return &items[0], nil
}
