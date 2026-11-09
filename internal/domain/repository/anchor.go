package repository

import (
	"context"
	"errors"
	"time"

	"smapass-back/internal/domain/entity"
)

// ErrRejected marks a provider response that will not succeed on retry (4xx, mismatched record).
var ErrRejected = errors.New("rejected by chain provider")

// MerkleBuilder turns payload hashes (in leaf order) into a root and one proof per leaf.
type MerkleBuilder func([][32]byte) ([32]byte, [][]entity.ProofStep, error)

type AnchorRepository interface {
	// CreateBatch groups up to limit signature-verified, unanchored logs into one pending anchor.
	// It returns nil when there is nothing to anchor.
	CreateBatch(ctx context.Context, chain string, limit int, build MerkleBuilder) (*entity.Anchor, error)
	// ClaimDue leases anchors whose next attempt is due so that concurrent workers skip them.
	ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]entity.Anchor, error)
	// MarkSubmitting records that a request is about to be sent. Only pending or submitting rows move.
	MarkSubmitting(ctx context.Context, id string) error
	MarkSubmitted(ctx context.Context, id string, receipt entity.TimestampReceipt, nextPoll time.Duration) error
	MarkConfirmed(ctx context.Context, id string, receipt entity.TimestampReceipt) error
	Reschedule(ctx context.Context, id string, delay time.Duration, reason string) error
	MarkFailed(ctx context.Context, id string, reason string) error
}

// TimestampClient records a 32-byte root on chain and reports its state.
type TimestampClient interface {
	Submit(ctx context.Context, root [32]byte) (*entity.TimestampReceipt, error)
	Find(ctx context.Context, ref string) (*entity.TimestampReceipt, error)
	// FindByRoot looks for an earlier submission of the same root; nil when none exists.
	FindByRoot(ctx context.Context, root [32]byte) (*entity.TimestampReceipt, error)
}
