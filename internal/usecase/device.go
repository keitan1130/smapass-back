package usecase

import (
	"context"
	"fmt"
	"slices"
	"time"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
)

func (u *DeviceUsecase) Ingest(ctx context.Context, principal entity.Principal, inputIMEI string, input entity.SubmitMeasurement) (*entity.MeasurementReceipt, error) {
	if !validPrincipal(principal) {
		return nil, entity.ErrUnauthenticated
	}
	imei, err := entity.NormalizeIMEI(inputIMEI)
	if err != nil {
		return nil, err
	}
	log, err := entity.PrepareMeasurement(input, time.Now())
	if err != nil {
		return nil, err
	}
	return u.repo.AppendMeasurement(ctx, principal, imei, log)
}

type DeviceUsecase struct {
	repo repository.DeviceRepository
	keys *KeyRegistrationUsecase
}

func NewDeviceUsecase(repo repository.DeviceRepository, keys *KeyRegistrationUsecase) *DeviceUsecase {
	return &DeviceUsecase{repo: repo, keys: keys}
}

func validPrincipal(p entity.Principal) bool {
	return p.Issuer == entity.DeviceIssuer && p.Subject != "" && p.SessionID != "" && p.IMEI != ""
}

func (u *DeviceUsecase) Register(ctx context.Context, principal entity.Principal, input entity.RegisterDevice) (*entity.Device, error) {
	if !validPrincipal(principal) {
		return nil, entity.ErrInvalidDevice
	}
	if err := u.keys.Verify(ctx, "rotate", principal, &input); err != nil {
		return nil, err
	}
	return u.repo.Register(ctx, principal, input)
}

func (u *DeviceUsecase) Challenge(ctx context.Context, p entity.Principal, input entity.RotationChallengeRequest) (*entity.KeyChallenge, error) {
	return u.keys.RotationChallenge(ctx, p, input)
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

func (u *DeviceUsecase) State(ctx context.Context, input string, limit, offset int) (*entity.DeviceState, error) {
	id, err := entity.NormalizeDeviceID(input)
	if err != nil {
		return nil, err
	}
	return u.repo.FindState(ctx, id, limit, offset)
}

func (u *DeviceUsecase) Records(ctx context.Context, input string, limit, offset int) ([]entity.VerificationRecord, int, error) {
	id, err := entity.NormalizeDeviceID(input)
	if err != nil {
		return nil, 0, err
	}
	return u.repo.Records(ctx, id, limit, offset)
}

func (u *DeviceUsecase) List(ctx context.Context, inputs []string) ([]entity.DeviceSummary, error) {
	if len(inputs) < 1 || len(inputs) > 50 {
		return nil, fmt.Errorf("%w: specify 1 to 50 UUIDs", entity.ErrInvalidDeviceID)
	}
	ids := make([]string, 0, len(inputs))
	for _, input := range inputs {
		id, err := entity.NormalizeDeviceID(input)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return u.repo.ListStates(ctx, ids)
}

func (u *DeviceUsecase) Search(ctx context.Context, inputs []string) ([]entity.DeviceReference, error) {
	if len(inputs) < 1 || len(inputs) > 50 {
		return nil, fmt.Errorf("%w: specify 1 to 50 IMEIs", entity.ErrInvalidDevice)
	}
	imeis := make([]string, 0, len(inputs))
	for _, input := range inputs {
		imei, err := entity.NormalizeIMEI(input)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(imeis, imei) {
			imeis = append(imeis, imei)
		}
	}
	return u.repo.Search(ctx, imeis)
}
