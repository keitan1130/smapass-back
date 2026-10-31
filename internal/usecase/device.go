package usecase

import (
	"context"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
)

type DeviceUsecase struct{ repo repository.DeviceRepository }

func NewDeviceUsecase(repo repository.DeviceRepository) *DeviceUsecase {
	return &DeviceUsecase{repo: repo}
}

func validPrincipal(p entity.Principal) bool {
	return p.Issuer != "" && p.Subject != "" && len(p.Issuer) <= 512 && len(p.Subject) <= 255
}

func (u *DeviceUsecase) Register(ctx context.Context, principal entity.Principal, input entity.RegisterDevice) (*entity.Device, error) {
	if !validPrincipal(principal) {
		return nil, entity.ErrInvalidDevice
	}
	if err := input.Normalize(); err != nil {
		return nil, err
	}
	return u.repo.Register(ctx, principal, input)
}

func (u *DeviceUsecase) Lookup(ctx context.Context, principal entity.Principal, input string) (*entity.Device, error) {
	if !validPrincipal(principal) {
		return nil, entity.ErrInvalidDevice
	}
	imei, err := entity.NormalizeIMEI(input)
	if err != nil {
		return nil, err
	}
	return u.repo.FindOwned(ctx, principal, imei)
}

func (u *DeviceUsecase) Revoke(ctx context.Context, principal entity.Principal, input string) error {
	if !validPrincipal(principal) {
		return entity.ErrInvalidDevice
	}
	imei, err := entity.NormalizeIMEI(input)
	if err != nil {
		return err
	}
	return u.repo.Revoke(ctx, principal, imei)
}
