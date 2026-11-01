// Package authorization centralizes checks repeated inside write transactions.
package authorization

import (
	"context"
	"database/sql"
	"errors"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
)

// RequireDevice must follow the device row lock. Holding a shared installation
// lock makes credential revocation/rotation and writes serialize with each other.
func RequireDevice(ctx context.Context, tx bun.Tx, p entity.Principal, imei string) error {
	if p.SessionID == "" || p.Issuer != entity.DeviceIssuer {
		return entity.ErrUnauthenticated
	}
	var active bool
	err := tx.NewRaw(`SELECT true FROM app_installations i JOIN app_tokens t ON t.installation_id=i.id
		JOIN device_ownerships o ON o.id=i.ownership_id AND o.ended_at IS NULL
		WHERE i.id::text=? AND i.imei=? AND i.revoked_at IS NULL
		AND o.owner_issuer=? AND o.owner_subject=i.id::text
		AND t.id::text=? AND t.rotated_at IS NULL AND t.access_expires_at>clock_timestamp()
		FOR SHARE OF i`, p.Subject, imei, entity.DeviceIssuer, p.SessionID).Scan(ctx, &active)
	if errors.Is(err, sql.ErrNoRows) {
		if p.IMEI != imei {
			return repository.ErrNotFound
		}
		return entity.ErrUnauthenticated
	}
	return err
}
