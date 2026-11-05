package dto

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"smapass-back/internal/domain/entity"
)

// Feed actual database column names and values through Bun's SQL scanner without
// requiring Docker. This reproduces the unit-suffix mismatch from integration.
func TestStatusSQLColumnsScanAndPreserveNulls(t *testing.T) {
	db := bun.NewDB(sql.OpenDB(statusConnector{}), pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })
	var rows []StatusRecord
	if err := db.NewRaw("SELECT * FROM device_status").Scan(context.Background(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("row count: %d", len(rows))
	}
	temperature, thermal := 32.3, 0.3
	cycles, charge, fullCharge, boots := 162, 3255, 4387, 46
	abnormal := false
	want := entity.StatusRecord{
		Sequence: 27, CollectedAt: statusCollectedAt, OSVersion: "16", SDKInt: 36,
		BatteryTempC: &temperature, BatteryCycleCount: &cycles,
		BatteryChargeMAh: &charge, BatteryFullChargeMAh: &fullCharge,
		BootCount: &boots, HadAbnormalExit: &abnormal, ThermalHeadroom: &thermal,
	}
	if got := rows[0].Entity(); !reflect.DeepEqual(got, want) {
		t.Fatalf("measurements changed during scan: got %+v want %+v", got, want)
	}
	want = entity.StatusRecord{Sequence: 28, CollectedAt: statusCollectedAt, OSVersion: "16", SDKInt: 36}
	if got := rows[1].Entity(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SQL NULL measurements were replaced: %+v", got)
	}
}

var statusCollectedAt = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)

type statusConnector struct{}

func (statusConnector) Connect(context.Context) (driver.Conn, error) { return statusConnection{}, nil }
func (statusConnector) Driver() driver.Driver                        { return statusDriver{} }

type statusDriver struct{}

func (statusDriver) Open(string) (driver.Conn, error) { return statusConnection{}, nil }

type statusConnection struct{}

func (statusConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not used by this fixture")
}
func (statusConnection) Close() error { return nil }
func (statusConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not used by this fixture")
}
func (statusConnection) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &statusRows{}, nil
}

type statusRows struct{ index int }

func (*statusRows) Columns() []string {
	return []string{"sequence", "collected_at", "os_version", "sdk_int", "battery_temp_c",
		"battery_cycle_count", "battery_charge_mah", "battery_full_charge_mah", "boot_count",
		"had_abnormal_exit", "thermal_headroom"}
}
func (*statusRows) Close() error { return nil }
func (r *statusRows) Next(dest []driver.Value) error {
	if r.index >= 2 {
		return io.EOF
	}
	values := []driver.Value{int64(27 + r.index), statusCollectedAt, "16", int64(36), nil, nil, nil, nil, nil, nil, nil}
	if r.index == 0 {
		copy(values[4:], []driver.Value{32.3, int64(162), int64(3255), int64(4387), int64(46), false, 0.3})
	}
	copy(dest, values)
	r.index++
	return nil
}
