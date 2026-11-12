package usecase

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/pkg/merkle"
)

type AnchorOptions struct {
	Chain        string
	MaxLeaves    int
	ClaimLimit   int
	Lease        time.Duration
	PollInterval time.Duration
	RetryBase    time.Duration
	RetryMax     time.Duration
}

func (o AnchorOptions) withDefaults() AnchorOptions {
	if o.MaxLeaves <= 0 {
		o.MaxLeaves = 1000
	}
	if o.ClaimLimit <= 0 {
		o.ClaimLimit = 20
	}
	if o.Lease <= 0 {
		o.Lease = 2 * time.Minute
	}
	if o.PollInterval <= 0 {
		o.PollInterval = 30 * time.Second
	}
	if o.RetryBase <= 0 {
		o.RetryBase = 30 * time.Second
	}
	if o.RetryMax <= 0 {
		o.RetryMax = time.Hour
	}
	return o
}

// AnchorUsecase sends Merkle roots of verified logs to the chain and tracks block inclusion.
// It never talks to the chain inside a database transaction.
type AnchorUsecase struct {
	repo   repository.AnchorRepository
	client repository.TimestampClient
	opt    AnchorOptions
}

func NewAnchorUsecase(repo repository.AnchorRepository, client repository.TimestampClient, opt AnchorOptions) *AnchorUsecase {
	return &AnchorUsecase{repo: repo, client: client, opt: opt.withDefaults()}
}

// Batch creates at most one pending anchor from unanchored verified logs.
func (u *AnchorUsecase) Batch(ctx context.Context) (*entity.Anchor, error) {
	return u.repo.CreateBatch(ctx, u.opt.Chain, u.opt.MaxLeaves, BuildMerkleV1)
}

// BuildMerkleV1 adapts pkg/merkle (smapass-merkle-v1) to the hex proof steps returned by the records API.
func BuildMerkleV1(hashes [][32]byte) ([32]byte, [][]entity.ProofStep, error) {
	root, proofs, err := merkle.Build(hashes)
	if err != nil {
		return root, nil, err
	}
	out := make([][]entity.ProofStep, len(proofs))
	for i, proof := range proofs {
		out[i] = make([]entity.ProofStep, len(proof))
		for j, step := range proof {
			out[i][j] = entity.ProofStep{Side: step.Side, Hash: hex.EncodeToString(step.Hash[:])}
		}
	}
	return root, out, nil
}

// Process advances every due anchor by one step. Per-anchor errors are rescheduled,
// not returned, so one bad anchor does not block the others.
func (u *AnchorUsecase) Process(ctx context.Context) (int, error) {
	anchors, err := u.repo.ClaimDue(ctx, u.opt.ClaimLimit, u.opt.Lease)
	if err != nil {
		return 0, err
	}
	for _, anchor := range anchors {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if err := u.step(ctx, anchor); err != nil {
			return 0, err
		}
	}
	return len(anchors), nil
}

func (u *AnchorUsecase) step(ctx context.Context, anchor entity.Anchor) error {
	switch anchor.Status {
	case entity.AnchorPending:
		return u.submit(ctx, anchor)
	case entity.AnchorSubmitting:
		// The previous attempt may have reached the provider before we crashed or timed out.
		found, err := u.client.FindByRoot(ctx, anchor.MerkleRoot)
		if err != nil {
			return u.retry(ctx, anchor, err)
		}
		if found != nil {
			return u.apply(ctx, anchor, *found)
		}
		return u.submit(ctx, anchor)
	case entity.AnchorSubmitted:
		if anchor.ProviderRef == nil {
			return u.repo.MarkFailed(ctx, anchor.ID, "submitted anchor has no provider reference")
		}
		receipt, err := u.client.Find(ctx, *anchor.ProviderRef)
		if err != nil {
			return u.retry(ctx, anchor, err)
		}
		return u.apply(ctx, anchor, *receipt)
	default:
		return nil
	}
}

func (u *AnchorUsecase) submit(ctx context.Context, anchor entity.Anchor) error {
	if err := u.repo.MarkSubmitting(ctx, anchor.ID); err != nil {
		return err
	}
	receipt, err := u.client.Submit(ctx, anchor.MerkleRoot)
	if err != nil {
		return u.retry(ctx, anchor, err)
	}
	return u.apply(ctx, anchor, *receipt)
}

func (u *AnchorUsecase) apply(ctx context.Context, anchor entity.Anchor, receipt entity.TimestampReceipt) error {
	if receipt.Ref == "" {
		return u.retry(ctx, anchor, fmt.Errorf("provider returned no reference: %w", repository.ErrRejected))
	}
	if receipt.Confirmed() {
		return u.repo.MarkConfirmed(ctx, anchor.ID, receipt)
	}
	return u.repo.MarkSubmitted(ctx, anchor.ID, receipt, u.opt.PollInterval)
}

func (u *AnchorUsecase) retry(ctx context.Context, anchor entity.Anchor, cause error) error {
	if ctx.Err() != nil {
		// Shutting down: leave the row as is; the lease expires and the next run reconciles it.
		return ctx.Err()
	}
	if errors.Is(cause, repository.ErrRejected) {
		return u.repo.MarkFailed(ctx, anchor.ID, cause.Error())
	}
	return u.repo.Reschedule(ctx, anchor.ID, u.backoff(anchor.RetryCount), cause.Error())
}

func (u *AnchorUsecase) backoff(retries int) time.Duration {
	delay := u.opt.RetryBase
	for i := 0; i < retries && delay < u.opt.RetryMax; i++ {
		delay *= 2
	}
	return min(delay, u.opt.RetryMax)
}
