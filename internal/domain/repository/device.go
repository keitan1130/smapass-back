package repository

import (
	"context"
	"smapass-back/internal/domain/entity"
)

type DeviceRepository interface {
	AppendMeasurement(context.Context, entity.Principal, string, *entity.MeasurementLog) (*entity.MeasurementReceipt, error)
	FindState(context.Context, string) (*entity.DeviceState, error)
	StatusHistory(context.Context, string, int, int) ([]entity.StatusRecord, int, error)
	Register(context.Context, entity.Principal, entity.RegisterDevice) (*entity.Device, error)
	FindOwned(context.Context, entity.Principal, string) (*entity.Device, error)
	Revoke(context.Context, entity.Principal, string) error
}
