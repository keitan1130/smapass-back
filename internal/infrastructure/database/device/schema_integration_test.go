//go:build integration

package device

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"smapass-back/internal/infrastructure/database/testutil"
)

func TestConsolidatedSchemaAndPublicUUID(t *testing.T) {
	db := testutil.NewDatabase(t)
	ctx := context.Background()
	r := NewRepository(db)
	id := publicID(t, db, "000000000094821")
	if parsed, err := uuid.Parse(id); err != nil || parsed.Version() != 4 {
		t.Fatalf("not a random UUID: %q %v", id, err)
	}
	items, err := r.ListStates(ctx, []string{id, id, "11111111-1111-4111-8111-111111111111"})
	if err != nil || len(items) != 1 || items[0].ID != id || !items[0].IsDemo {
		t.Fatalf("summary deduplication: %v %v", items, err)
	}
	// Every seeded log is available for independent verification.
	logs, total, err := r.Records(ctx, id, 2, 27)
	if err != nil || total != 28 || len(logs) != 1 || logs[0].Sequence != 28 {
		t.Fatalf("lifetime evidence: %d %v", total, err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE device_logs SET signature=decode('01','hex') WHERE imei='000000000094821'"); err == nil {
		t.Fatal("signed log became mutable")
	}
}
