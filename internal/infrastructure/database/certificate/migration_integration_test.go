//go:build integration

package certificate

import (
	"context"
	"testing"

	"smapass-back/db/migrations"
	"smapass-back/internal/infrastructure/database/testutil"
)

func TestIMEICertificateMigrationPreservesSnapshotsAndRollback(t *testing.T) {
	db := testutil.NewDatabaseAtVersion(t, 4)
	ctx := context.Background()
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = conn.ExecContext(ctx, `
		INSERT INTO devices (imei) VALUES ('490154203237518');
		INSERT INTO device_imei_aliases VALUES ('490154203237518','490154203237518');
		INSERT INTO device_ownerships (imei,owner_issuer,owner_subject) VALUES ('490154203237518','fixture','fixture');
		INSERT INTO certificates (id,imei,ownership_id,number,issued_at,snapshot)
		SELECT '11111111-1111-4111-8111-111111111111',imei,id,'01ABCDEFGH','2026-01-01','{"marker":1}' FROM device_ownerships;
		INSERT INTO certificates (id,imei,ownership_id,number,issued_at,snapshot,status,successor_number)
		SELECT '22222222-2222-4222-8222-222222222222',imei,id,'02ABCDEFGH','2025-01-01','{"marker":2}','superseded','01ABCDEFGH' FROM device_ownerships;`)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(file string) {
		t.Helper()
		data, err := migrations.Files.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, string(data)); err != nil {
			t.Fatal(err)
		}
	}
	apply("000005_imei_certificates.up.sql")
	var successor, snapshot string
	if err := conn.QueryRowContext(ctx, `SELECT successor_id::text,snapshot::text FROM certificates
		WHERE id='22222222-2222-4222-8222-222222222222'`).Scan(&successor, &snapshot); err != nil || successor != "11111111-1111-4111-8111-111111111111" || snapshot != `{"marker": 2}` {
		t.Fatalf("snapshot or successor changed: %s %s %v", successor, snapshot, err)
	}
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name='certificates'
		AND column_name IN ('number','successor_number')`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("public number columns retained: %d %v", count, err)
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO certificates (id,imei,ownership_id,issued_at,snapshot)
		SELECT '33333333-3333-4333-8333-333333333333',imei,id,'2026-02-01','{"marker":3}' FROM device_ownerships`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE certificates SET snapshot='{}' WHERE id='11111111-1111-4111-8111-111111111111'`); err == nil {
		t.Fatal("issued snapshot became mutable")
	}
	apply("000005_imei_certificates.down.sql")
	var number string
	if err := conn.QueryRowContext(ctx, `SELECT number FROM certificates WHERE id='11111111-1111-4111-8111-111111111111'`).Scan(&number); err != nil || number != "01ABCDEFGH" {
		t.Fatalf("legacy number not restored: %s %v", number, err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT count(DISTINCT number) FROM certificates`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("new certificate rollback: %d %v", count, err)
	}
	apply("000005_imei_certificates.up.sql")
}
