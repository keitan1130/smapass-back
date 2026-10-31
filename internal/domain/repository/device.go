package repository

import (
	"context"
	"smapass-back/internal/domain/entity"
)

type DeviceRepository interface {
	Register(context.Context, entity.Principal, entity.RegisterDevice) (*entity.Device, error)
	FindOwned(context.Context, entity.Principal, string) (*entity.Device, error)
	Revoke(context.Context, entity.Principal, string) error
}
