package dto

import (
	"time"

	"smapass-back/internal/domain/entity"
)

// StatusRecord maps SQL column names explicitly, including unit suffixes that
// Bun's automatic Go-name conversion does not preserve.
type StatusRecord struct {
	Sequence             int64     `bun:"sequence"`
	CollectedAt          time.Time `bun:"collected_at"`
	OSVersion            string    `bun:"os_version"`
	SDKInt               int       `bun:"sdk_int"`
	BatteryTempC         *float64  `bun:"battery_temp_c"`
	BatteryCycleCount    *int      `bun:"battery_cycle_count"`
	BatteryChargeMAh     *int      `bun:"battery_charge_mah"`
	BatteryFullChargeMAh *int      `bun:"battery_full_charge_mah"`
	BootCount            *int      `bun:"boot_count"`
	HadAbnormalExit      *bool     `bun:"had_abnormal_exit"`
	ThermalHeadroom      *float64  `bun:"thermal_headroom"`
}

func (s StatusRecord) Entity() entity.StatusRecord {
	return entity.StatusRecord{
		Sequence: s.Sequence, CollectedAt: s.CollectedAt,
		OSVersion: s.OSVersion, SDKInt: s.SDKInt,
		BatteryTempC: s.BatteryTempC, BatteryCycleCount: s.BatteryCycleCount,
		BatteryChargeMAh: s.BatteryChargeMAh, BatteryFullChargeMAh: s.BatteryFullChargeMAh,
		BootCount: s.BootCount, HadAbnormalExit: s.HadAbnormalExit,
		ThermalHeadroom: s.ThermalHeadroom,
	}
}
