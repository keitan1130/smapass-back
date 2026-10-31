package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"smapass-back/internal/domain/entity"
)

// Reject incomplete legacy mapping before migrate marks version 2 dirty.
func preflightIMEI(dsn string) error {
	database, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("cannot inspect legacy device migration")
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var legacy bool
	if err = database.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='devices' AND column_name='id')").Scan(&legacy); err != nil {
		return fmt.Errorf("cannot inspect legacy device migration")
	}
	if !legacy {
		return nil
	}
	var hasMap bool
	if err = database.QueryRowContext(ctx, "SELECT to_regclass('public.device_imei_migration_map') IS NOT NULL").Scan(&hasMap); err != nil {
		return fmt.Errorf("cannot inspect IMEI mapping")
	}
	mapping := map[string]string{}
	if hasMap {
		rows, err := database.QueryContext(ctx, "SELECT legacy_device_id, imei FROM device_imei_migration_map")
		if err != nil {
			return fmt.Errorf("cannot read IMEI mapping")
		}
		for rows.Next() {
			var id, imei string
			if err := rows.Scan(&id, &imei); err != nil {
				_ = rows.Close()
				return err
			}
			if normalized, err := entity.NormalizeIMEI(imei); err != nil || normalized != imei {
				_ = rows.Close()
				return fmt.Errorf("IMEI mapping contains an invalid full IMEI")
			}
			mapping[id] = imei
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return fmt.Errorf("cannot read IMEI mapping")
		}
	}
	rows, err := database.QueryContext(ctx, `SELECT d.id,
		d.id='d9aaf2bc-6c41-51ed-b18a-2f567dd6075a'
		AND EXISTS (SELECT 1 FROM certificates c WHERE c.device_id=d.id AND c.is_demo)
		AND NOT EXISTS (SELECT 1 FROM certificates c WHERE c.device_id=d.id AND NOT c.is_demo)
		AND EXISTS (SELECT 1 FROM device_keys k WHERE k.device_id=d.id AND k.public_key='DEMO-NOT-A-PUBLIC-KEY')
		FROM devices d`)
	if err != nil {
		return fmt.Errorf("cannot inspect legacy devices")
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var demo bool
		if err := rows.Scan(&id, &demo); err != nil {
			return err
		}
		if !demo && mapping[id] == "" {
			return fmt.Errorf("verified full IMEI mapping required for legacy device %s; see db/prepare-imei-migration.sql", id)
		}
	}
	return rows.Err()
}
