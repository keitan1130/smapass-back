//go:build integration

package certificate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"smapass-back/internal/infrastructure/database/testutil"
)

// The DB is created, migrated, seeded and removed automatically for this test.
func TestDevelopmentCertificate(t *testing.T) {
	database := testutil.NewDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := NewRepository(database)
	cert, err := repo.FindByNumber(ctx, "01ABCDEFGH")
	if err != nil {
		t.Fatal(err)
	}
	if !cert.IsDemo || cert.Snapshot.Device.Model != "Pixel 8" || cert.Listing == nil || cert.Listing.IMEISuffix == nil || *cert.Listing.IMEISuffix != "4821" {
		t.Fatalf("unexpected certificate: %+v", cert)
	}
	items, err := repo.FindByNumbers(ctx, []string{"01ABCDEFGH", "0000000000"})
	if err != nil || len(items) != 1 {
		t.Fatalf("batch: items=%d err=%v", len(items), err)
	}
	records, total, err := repo.Records(ctx, "01ABCDEFGH", 100, 0)
	if err != nil || total != 28 || len(records) != 28 {
		t.Fatalf("records: count=%d total=%d err=%v", len(records), total, err)
	}
	for index, record := range records {
		hash := sha256.Sum256([]byte(record.CanonicalPayload))
		if record.PayloadHash != hex.EncodeToString(hash[:]) {
			t.Fatalf("hash mismatch at %d", index)
		}
		if index > 0 && (record.PreviousHash == nil || *record.PreviousHash != records[index-1].PayloadHash) {
			t.Fatalf("chain mismatch at %d", index)
		}
		if record.AnchorState != "unanchored" || record.TransactionHash != nil || record.Chain != nil {
			t.Fatal("demo must not claim anchoring")
		}
	}
	page, total, err := repo.Records(ctx, "01ABCDEFGH", 2, 27)
	if err != nil || total != 28 || len(page) != 1 || page[0].Sequence != 28 {
		t.Fatalf("pagination failed: %+v %d %v", page, total, err)
	}
}

func TestEvidenceAndSnapshotAreImmutable(t *testing.T) {
	database := testutil.NewDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, test := range []struct{ query, message string }{
		{"UPDATE device_logs SET collected_at = collected_at + interval '1 day' WHERE seq = 1", "signed evidence is append-only"},
		{"DELETE FROM device_logs WHERE seq = 1", "signed evidence is append-only"},
		{"UPDATE certificates SET snapshot = '{}'::jsonb WHERE number = '01ABCDEFGH'", "issue a new certificate to change snapshot"},
	} {
		if _, err := database.ExecContext(ctx, test.query); err == nil || !strings.Contains(err.Error(), test.message) {
			t.Fatalf("mutation was not rejected as expected: %v", err)
		}
	}
}
