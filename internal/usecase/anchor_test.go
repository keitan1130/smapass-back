package usecase

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
)

type anchorRepo struct {
	repository.AnchorRepository
	due     []entity.Anchor
	events  []string
	delay   time.Duration
	receipt entity.TimestampReceipt
}

func (r *anchorRepo) ClaimDue(context.Context, int, time.Duration) ([]entity.Anchor, error) {
	return r.due, nil
}
func (r *anchorRepo) MarkSubmitting(_ context.Context, id string) error {
	r.events = append(r.events, "submitting:"+id)
	return nil
}
func (r *anchorRepo) MarkSubmitted(_ context.Context, id string, receipt entity.TimestampReceipt, _ time.Duration) error {
	r.events, r.receipt = append(r.events, "submitted:"+id), receipt
	return nil
}
func (r *anchorRepo) MarkConfirmed(_ context.Context, id string, receipt entity.TimestampReceipt) error {
	r.events, r.receipt = append(r.events, "confirmed:"+id), receipt
	return nil
}
func (r *anchorRepo) Reschedule(_ context.Context, id string, delay time.Duration, _ string) error {
	r.events, r.delay = append(r.events, "reschedule:"+id), delay
	return nil
}
func (r *anchorRepo) MarkFailed(_ context.Context, id string, _ string) error {
	r.events = append(r.events, "failed:"+id)
	return nil
}

type chainClient struct {
	submits     int
	submitErr   error
	submitted   *entity.TimestampReceipt
	existing    *entity.TimestampReceipt
	found       *entity.TimestampReceipt
	findErr     error
	findByRoots int
}

func (c *chainClient) Submit(context.Context, [32]byte) (*entity.TimestampReceipt, error) {
	c.submits++
	return c.submitted, c.submitErr
}
func (c *chainClient) Find(context.Context, string) (*entity.TimestampReceipt, error) {
	return c.found, c.findErr
}
func (c *chainClient) FindByRoot(context.Context, [32]byte) (*entity.TimestampReceipt, error) {
	c.findByRoots++
	return c.existing, nil
}

func ptr[T any](v T) *T { return &v }

func confirmedReceipt() *entity.TimestampReceipt {
	return &entity.TimestampReceipt{Ref: "7", TxID: ptr("ab"), Status: entity.TimestampConfirmed,
		BlockHeight: ptr(int64(101)), BlockTime: ptr(time.Unix(1626169080, 0))}
}

func run(t *testing.T, repo *anchorRepo, client *chainClient) {
	t.Helper()
	u := NewAnchorUsecase(repo, client, AnchorOptions{Chain: "tapyrus"})
	if _, err := u.Process(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func eq(t *testing.T, got []string, want ...string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events %v, want %v", got, want)
	}
}

func TestPendingIsMarkedBeforeSendingAndWaitsForBlock(t *testing.T) {
	repo := &anchorRepo{due: []entity.Anchor{{ID: "a", Status: entity.AnchorPending}}}
	client := &chainClient{submitted: &entity.TimestampReceipt{Ref: "7", Status: entity.TimestampUnconfirmed, TxID: ptr("ab")}}
	run(t, repo, client)
	eq(t, repo.events, "submitting:a", "submitted:a")
}

func TestReceiptIsConfirmedOnlyWithBlockData(t *testing.T) {
	incomplete := confirmedReceipt()
	incomplete.BlockTime = nil
	repo := &anchorRepo{due: []entity.Anchor{{ID: "a", Status: entity.AnchorSubmitted, ProviderRef: ptr("7")}}}
	run(t, repo, &chainClient{found: incomplete})
	eq(t, repo.events, "submitted:a")

	repo = &anchorRepo{due: []entity.Anchor{{ID: "a", Status: entity.AnchorSubmitted, ProviderRef: ptr("7")}}}
	run(t, repo, &chainClient{found: confirmedReceipt()})
	eq(t, repo.events, "confirmed:a")
	if *repo.receipt.BlockHeight != 101 {
		t.Fatal("block data must be stored")
	}
}

func TestInterruptedSubmissionIsReconciledWithoutResending(t *testing.T) {
	repo := &anchorRepo{due: []entity.Anchor{{ID: "a", Status: entity.AnchorSubmitting}}}
	client := &chainClient{existing: &entity.TimestampReceipt{Ref: "7", Status: entity.TimestampInit}}
	run(t, repo, client)
	if client.submits != 0 || client.findByRoots != 1 {
		t.Fatalf("submits=%d lookups=%d", client.submits, client.findByRoots)
	}
	eq(t, repo.events, "submitted:a")
}

func TestInterruptedSubmissionNotFoundIsResent(t *testing.T) {
	repo := &anchorRepo{due: []entity.Anchor{{ID: "a", Status: entity.AnchorSubmitting}}}
	client := &chainClient{submitted: confirmedReceipt()}
	run(t, repo, client)
	if client.submits != 1 {
		t.Fatalf("submits=%d", client.submits)
	}
	eq(t, repo.events, "submitting:a", "confirmed:a")
}

func TestTransientErrorBacksOffAndRejectionFails(t *testing.T) {
	repo := &anchorRepo{due: []entity.Anchor{{ID: "a", Status: entity.AnchorPending, RetryCount: 3}}}
	run(t, repo, &chainClient{submitErr: errors.New("503")})
	eq(t, repo.events, "submitting:a", "reschedule:a")
	if repo.delay != 4*time.Minute {
		t.Fatalf("backoff %s", repo.delay)
	}

	repo = &anchorRepo{due: []entity.Anchor{{ID: "a", Status: entity.AnchorPending, RetryCount: 30}}}
	run(t, repo, &chainClient{submitErr: errors.New("503")})
	if repo.delay != time.Hour {
		t.Fatalf("backoff cap %s", repo.delay)
	}

	repo = &anchorRepo{due: []entity.Anchor{{ID: "a", Status: entity.AnchorPending}}}
	run(t, repo, &chainClient{submitErr: fmt.Errorf("400: %w", repository.ErrRejected)})
	eq(t, repo.events, "submitting:a", "failed:a")
}

func TestShutdownLeavesAnchorForReconciliation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repo := &anchorRepo{due: []entity.Anchor{{ID: "a", Status: entity.AnchorPending}}}
	client := &chainClient{submitErr: context.Canceled}
	u := NewAnchorUsecase(repo, client, AnchorOptions{})
	cancel()
	if _, err := u.Process(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if len(repo.events) != 0 {
		t.Fatalf("no state change expected: %v", repo.events)
	}
}

func TestBuildMerkleV1ReturnsHexProofs(t *testing.T) {
	root, proofs, err := BuildMerkleV1([][32]byte{{1}, {2}, {3}})
	if err != nil || root == ([32]byte{}) || len(proofs) != 3 || len(proofs[0][0].Hash) != 64 || proofs[2][0].Side != "left" {
		t.Fatalf("root=%x proofs=%v err=%v", root, proofs, err)
	}
}
