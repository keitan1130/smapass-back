package repository

import (
	"context"
	"smapass-back/internal/domain/entity"
)

//go:generate go tool mockgen -source=device.go -destination=mock/device_mock.go -package=mock -self_package=smapass-back/internal/domain/repository/mock -write_command_comment=false
type DeviceRepository interface {
	Search(context.Context, []string) ([]entity.DeviceReference, error)
	ListStates(context.Context, []string) ([]entity.DeviceSummary, error)
	AppendMeasurement(context.Context, entity.Principal, string, *entity.MeasurementLog) (*entity.MeasurementReceipt, error)
	FindState(context.Context, string, int, int) (*entity.DeviceState, error)
	Records(context.Context, string, int, int) ([]entity.VerificationRecord, int, error)
	Register(context.Context, entity.Principal, entity.RegisterDevice) (*entity.Device, error)
	FindOwned(context.Context, entity.Principal, string) (*entity.Device, error)
	Revoke(context.Context, entity.Principal, string) error
}
