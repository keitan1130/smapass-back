//go:build integration

package certificate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"smapass-back/pkg/db"
)

// Uses an isolated development DB with migrations and development seed already applied.
func TestDevelopmentCertificate(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Fatal("TEST_DB_DSN is required with -tags=integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	database, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
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
