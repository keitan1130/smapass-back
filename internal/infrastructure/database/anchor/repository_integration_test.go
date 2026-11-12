//go:build integration

package anchor

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"smapass-back/internal/domain/entity"
	authdb "smapass-back/internal/infrastructure/database/auth"
	"smapass-back/internal/infrastructure/database/device"
	"smapass-back/internal/infrastructure/database/testutil"
	"smapass-back/internal/usecase"
	"smapass-back/pkg/merkle"
)

type fixture struct {
	db     *bun.DB
	device *entity.Device
}

func setup(t *testing.T) fixture {
	t.Helper()
	db := testutil.NewDatabase(t) // includes the unverified demo logs, which must never be anchored
	ctx := context.Background()
	auth := usecase.NewAuthUsecase(authdb.NewRepository(db), testutil.Registration(t, db))
	pair, err := auth.Enroll(ctx, testutil.Enrollment(t, db, "490154203237518"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := auth.Authenticate(ctx, pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	devices := device.NewRepository(db)
	if _, err := devices.SetVisibility(ctx, p, p.DeviceID, "public"); err != nil {
		t.Fatal(err)
	}
	d, err := usecase.NewDeviceUsecase(devices, testutil.Registration(t, db)).Register(ctx, p, testutil.Rotation(t, db, p, p.DeviceID))
	if err != nil {
		t.Fatal(err)
	}
	return fixture{db: db, device: d}
}

// insertLog is a database-level fixture, not a signature-verification implementation.
func (f fixture) insertLog(t *testing.T, seq int, verified bool) {
	t.Helper()
	var marker any
	if verified {
		marker = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	payload := `{"seq":` + strconv.Itoa(seq) + `}`
	_, err := f.db.ExecContext(context.Background(), `INSERT INTO device_logs (device_id, ownership_id, key_id, seq, type,
		canonicalization, canonical_payload, payload, payload_hash, prev_hash, signature, collected_at, received_at, signature_verified_at)
		VALUES (?, ?, ?, ?, 'status', 'test', ?, ?::jsonb, sha256(convert_to(?, 'UTF8')),
		(SELECT payload_hash FROM device_logs WHERE device_id=? ORDER BY seq DESC LIMIT 1), decode('01','hex'), '2000-01-01',
		clock_timestamp(), ?)`,
		f.device.DeviceID, f.device.OwnershipID, f.device.KeyID, seq, payload, payload, payload, f.device.DeviceID, marker)
	if err != nil {
		t.Fatal(err)
	}
}

func (f fixture) states(t *testing.T) []string {
	t.Helper()
	records, _, err := device.NewRepository(f.db).Records(context.Background(), f.device.DeviceID, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = r.AnchorState
	}
	return out
}

func TestBatchOnlyVerifiedLogsWithVerifiableProofs(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.insertLog(t, 1, true)
	f.insertLog(t, 2, false)
	f.insertLog(t, 3, true)
	f.insertLog(t, 4, true)
	repo := NewRepository(f.db)
	anchor, err := repo.CreateBatch(ctx, "tapyrus", 1000, usecase.BuildMerkleV1)
	if err != nil || anchor == nil || anchor.LeafCount != 3 || anchor.Status != entity.AnchorPending {
		t.Fatalf("anchor %+v err %v", anchor, err)
	}
	rows, err := f.db.QueryContext(ctx, `SELECT l.payload_hash, la.leaf_index, la.merkle_proof::text, l.seq
		FROM log_anchors la JOIN device_logs l ON l.id = la.log_id WHERE la.anchor_id = ? ORDER BY la.leaf_index`, anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var seqs []int
	for rows.Next() {
		var hash []byte
		var index, seq int
		var proofJSON string
		if err := rows.Scan(&hash, &index, &proofJSON, &seq); err != nil {
			t.Fatal(err)
		}
		var steps []entity.ProofStep
		if err := jsonUnmarshal(proofJSON, &steps); err != nil {
			t.Fatal(err)
		}
		proof := make([]merkle.Step, len(steps))
		for i, s := range steps {
			b, _ := hex.DecodeString(s.Hash)
			proof[i] = merkle.Step{Side: s.Side, Hash: [32]byte(b)}
		}
		if !merkle.Verify([32]byte(hash), proof, anchor.MerkleRoot) {
			t.Fatalf("leaf %d proof does not verify", index)
		}
		seqs = append(seqs, seq)
	}
	if len(seqs) != 3 || seqs[0] != 1 || seqs[1] != 3 || seqs[2] != 4 {
		t.Fatalf("anchored seqs %v (in receive order, verified only)", seqs)
	}
	again, err := repo.CreateBatch(ctx, "tapyrus", 1000, usecase.BuildMerkleV1)
	if err != nil || again != nil {
		t.Fatalf("second batch %+v %v", again, err)
	}
	var demoAnchored int
	if err := f.db.NewRaw(`SELECT count(*) FROM log_anchors la JOIN device_logs l ON l.id = la.log_id
		WHERE l.signature_verified_at IS NULL`).Scan(ctx, &demoAnchored); err != nil || demoAnchored != 0 {
		t.Fatalf("unverified logs anchored: %d %v", demoAnchored, err)
	}
}

func TestLifecycleLeaseAndPublicState(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.insertLog(t, 1, true)
	repo := NewRepository(f.db)
	anchor, err := repo.CreateBatch(ctx, "tapyrus", 1000, usecase.BuildMerkleV1)
	if err != nil || anchor == nil {
		t.Fatal(err)
	}
	if got := f.states(t); len(got) != 1 || got[0] != "pending" {
		t.Fatalf("pending state %v", got)
	}
	due, err := repo.ClaimDue(ctx, 10, time.Minute)
	if err != nil || len(due) != 1 || due[0].MerkleRoot != anchor.MerkleRoot {
		t.Fatalf("claim %+v %v", due, err)
	}
	if again, err := repo.ClaimDue(ctx, 10, time.Minute); err != nil || len(again) != 0 {
		t.Fatalf("leased anchor claimed twice: %+v %v", again, err)
	}
	if err := repo.MarkSubmitting(ctx, anchor.ID); err != nil {
		t.Fatal(err)
	}
	txid := "6fce02d39279f6d645ecc710ebcf1dbb7b8104106553d8da13f5db79c5a628fc"
	if err := repo.MarkSubmitted(ctx, anchor.ID, entity.TimestampReceipt{Ref: "7", TxID: &txid, Status: entity.TimestampUnconfirmed}, time.Second); err != nil {
		t.Fatal(err)
	}
	if got := f.states(t); got[0] != "pending" {
		t.Fatalf("submitted must read as pending, got %v", got)
	}
	if err := repo.MarkConfirmed(ctx, anchor.ID, entity.TimestampReceipt{Ref: "7", TxID: &txid, Status: entity.TimestampUnconfirmed}); err == nil {
		t.Fatal("unconfirmed receipt must not confirm")
	}
	height, blockTime := int64(101), time.Unix(1626169080, 0)
	if err := repo.MarkConfirmed(ctx, anchor.ID, entity.TimestampReceipt{Ref: "7", TxID: &txid, Status: entity.TimestampConfirmed, BlockHeight: &height, BlockTime: &blockTime}); err != nil {
		t.Fatal(err)
	}
	records, _, err := device.NewRepository(f.db).Records(ctx, f.device.DeviceID, 100, 0)
	if err != nil || records[0].AnchorState != "confirmed" || *records[0].TransactionHash != txid || *records[0].MerkleRoot != hex.EncodeToString(anchor.MerkleRoot[:]) {
		t.Fatalf("records %+v %v", records, err)
	}
	if err := repo.MarkFailed(ctx, anchor.ID, "late"); !errors.Is(err, errStateChanged) {
		t.Fatalf("confirmed anchor must not move: %v", err)
	}
	var stored struct {
		Height int64
		Time   time.Time
	}
	if err := f.db.NewRaw("SELECT block_height, block_time FROM anchors WHERE id=?", anchor.ID).Scan(ctx, &stored.Height, &stored.Time); err != nil ||
		stored.Height != 101 || !stored.Time.Equal(blockTime) {
		t.Fatalf("block data %+v %v", stored, err)
	}
}

func TestSchemaRejectsConfirmedWithoutBlockAndRescheduleCounts(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.insertLog(t, 1, true)
	repo := NewRepository(f.db)
	anchor, err := repo.CreateBatch(ctx, "tapyrus", 1000, usecase.BuildMerkleV1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, `UPDATE anchors SET status='confirmed', tx_id='x', confirmed_at=now() WHERE id=?`, anchor.ID); err == nil {
		t.Fatal("confirmed without block data must violate the schema")
	}
	if err := repo.Reschedule(ctx, anchor.ID, time.Hour, longJapanese()); err != nil {
		t.Fatal(err)
	}
	var retries int
	var due bool
	if err := f.db.NewRaw("SELECT retry_count, next_attempt_at <= now() FROM anchors WHERE id=?", anchor.ID).Scan(ctx, &retries, &due); err != nil || retries != 1 || due {
		t.Fatalf("retries=%d due=%v %v", retries, due, err)
	}
}

func longJapanese() string {
	s := ""
	for len(s) < 2*lastErrorLimit {
		s += "失敗"
	}
	return s
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
