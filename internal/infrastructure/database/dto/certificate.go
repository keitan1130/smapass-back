package dto

import (
	"time"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
)

type Certificate struct {
	bun.BaseModel `bun:"table:certificates,alias:c"`
	ID            string          `bun:"id"`
	IMEI          string          `bun:"imei"`
	Status        string          `bun:"status"`
	IssuedAt      time.Time       `bun:"issued_at"`
	IsDemo        bool            `bun:"is_demo"`
	Snapshot      entity.Snapshot `bun:"snapshot,type:jsonb"`
	Listing       *entity.Listing `bun:"listing,type:jsonb,scanonly"`
}

func (c Certificate) Entity() entity.Certificate {
	return entity.Certificate{ID: c.ID, IMEI: c.IMEI, Status: c.Status,
		IssuedAt: c.IssuedAt, IsDemo: c.IsDemo, Snapshot: c.Snapshot, Listing: c.Listing}
}
