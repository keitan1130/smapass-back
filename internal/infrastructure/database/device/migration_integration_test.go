//go:build integration

package device

import (
	"context"
	"testing"

	"smapass-back/db/migrations"
	"smapass-back/internal/infrastructure/database/testutil"
)

func TestLegacyIMEIMigrationPreservesEvidence(t *testing.T) {
	database := testutil.NewDatabaseAtVersion(t, 1)
	ctx := context.Background()
	conn, err := database.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = conn.ExecContext(ctx, `
	INSERT INTO devices (id, imei_suffix) VALUES ('d9aaf2bc-6c41-51ed-b18a-2f567dd6075a','4821');
	INSERT INTO device_keys (id, device_id, public_key, signature_algorithm)
	VALUES ('dc07a683-587e-5651-b11e-e84ff5987ad9','d9aaf2bc-6c41-51ed-b18a-2f567dd6075a','DEMO-NOT-A-PUBLIC-KEY','demo');
	INSERT INTO device_logs (id, device_id, key_id, seq, type, canonicalization, canonical_payload, payload, payload_hash, signature, collected_at)
	VALUES ('afc3fdcb-0505-5e9e-8c88-677166f3bf07','d9aaf2bc-6c41-51ed-b18a-2f567dd6075a','dc07a683-587e-5651-b11e-e84ff5987ad9',1,'status','demo','{}','{}',sha256(convert_to('{}','UTF8')),decode('01','hex'),'2026-04-06');
	INSERT INTO certificates (id,device_id,number,issued_at,is_demo,snapshot)
	VALUES ('50982a98-31c1-595e-a66d-c3688af07155','d9aaf2bc-6c41-51ed-b18a-2f567dd6075a','01ABCDEFGH',now(),true,'{}');`)
	if err != nil {
		t.Fatal(err)
	}
	up, err := migrations.Files.ReadFile("000002_imei_device_management.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := migrations.Files.ReadFile("000002_imei_device_management.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	var imei, hash string
	if err = conn.QueryRowContext(ctx, "SELECT imei, encode(payload_hash,'hex') FROM device_logs").Scan(&imei, &hash); err != nil {
		t.Fatal(err)
	}
	if imei != "000000000094821" || hash != "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a" {
		t.Fatalf("evidence changed: %s %s", imei, hash)
	}
	if _, err = conn.ExecContext(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	var restored string
	if err = conn.QueryRowContext(ctx, "SELECT device_id FROM device_logs").Scan(&restored); err != nil || restored != "d9aaf2bc-6c41-51ed-b18a-2f567dd6075a" {
		t.Fatalf("legacy mapping lost: %s %v", restored, err)
	}
	// Unknown real devices must never be assigned a guessed IMEI.
	if _, err = conn.ExecContext(ctx, "INSERT INTO devices (id) VALUES ('11111111-1111-4111-8111-111111111111')"); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, string(up)); err == nil {
		t.Fatal("unmapped legacy device was migrated")
	}
	if _, err = conn.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `CREATE TABLE device_imei_migration_map (legacy_device_id UUID PRIMARY KEY, imei TEXT UNIQUE NOT NULL);
	INSERT INTO device_imei_migration_map VALUES ('11111111-1111-4111-8111-111111111111','490154203237518')`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
}
