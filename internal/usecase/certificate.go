package usecase

import (
	"context"
	"fmt"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
)

type CertificateUsecase struct {
	repo repository.CertificateRepository
}

func NewCertificateUsecase(repo repository.CertificateRepository) *CertificateUsecase {
	return &CertificateUsecase{repo: repo}
}

func (u *CertificateUsecase) Get(ctx context.Context, input string) (*entity.Certificate, error) {
	imei, err := entity.NormalizeIMEI(input)
	if err != nil {
		return nil, err
	}
	return u.repo.FindLatestByIMEI(ctx, imei)
}

func (u *CertificateUsecase) List(ctx context.Context, inputs []string) ([]entity.Certificate, error) {
	if len(inputs) == 0 || len(inputs) > 50 {
		return nil, fmt.Errorf("%w: specify 1 to 50 IMEIs", entity.ErrInvalidDevice)
	}
	imeis := make([]string, 0, len(inputs))
	seen := make(map[string]bool)
	for _, input := range inputs {
		imei, err := entity.NormalizeIMEI(input)
		if err != nil {
			return nil, err
		}
		if !seen[imei] {
			imeis = append(imeis, imei)
			seen[imei] = true
		}
	}
	return u.repo.FindLatestByIMEIs(ctx, imeis)
}

func (u *CertificateUsecase) Records(ctx context.Context, input string, limit, offset int) ([]entity.VerificationRecord, int, error) {
	imei, err := entity.NormalizeIMEI(input)
	if err != nil {
		return nil, 0, err
	}
	return u.repo.Records(ctx, imei, limit, offset)
}

func (u *CertificateUsecase) CurrentByIMEI(ctx context.Context, input string) (*entity.Certificate, error) {
	imei, err := entity.NormalizeIMEI(input)
	if err != nil {
		return nil, err
	}
	return u.repo.FindLatestByIMEI(ctx, imei)
}

func (u *CertificateUsecase) Revoke(ctx context.Context, p entity.Principal, input string) error {
	if !validPrincipal(p) {
		return entity.ErrUnauthenticated
	}
	ref, err := entity.NormalizeIMEI(input)
	if err != nil {
		return err
	}
	return u.repo.RevokeOwned(ctx, p, ref)
}
