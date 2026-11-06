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
	var primaryKey string
	if err := db.NewRaw("SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='devices'::regclass AND contype='p'").Scan(ctx, &primaryKey); err != nil || primaryKey != "PRIMARY KEY (id)" {
		t.Fatalf("device primary key: %s %v", primaryKey, err)
	}
	var legacyColumns int
	if err := db.NewRaw(`SELECT count(*) FROM information_schema.columns WHERE table_schema='public'
        AND ((table_name='devices' AND column_name='public_id') OR
        (table_name IN ('device_ownerships','device_keys','device_logs','device_specs','device_status','app_installations') AND column_name='imei'))`).Scan(ctx, &legacyColumns); err != nil || legacyColumns != 0 {
		t.Fatalf("legacy identity columns: %d %v", legacyColumns, err)
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
	if _, err = db.ExecContext(ctx, "UPDATE device_logs SET signature=decode('01','hex') WHERE device_id=?", id); err == nil {
		t.Fatal("signed log became mutable")
	}
}
