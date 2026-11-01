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
	number, err := entity.NormalizeCertificateReference(input)
	if err != nil {
		return nil, err
	}
	return u.repo.FindByNumber(ctx, number)
}

func (u *CertificateUsecase) List(ctx context.Context, inputs []string) ([]entity.Certificate, error) {
	if len(inputs) == 0 || len(inputs) > 50 {
		return nil, fmt.Errorf("%w: specify 1 to 50 ids", entity.ErrInvalidNumber)
	}
	numbers := make([]string, 0, len(inputs))
	seen := make(map[string]bool)
	for _, input := range inputs {
		number, err := entity.NormalizeCertificateReference(input)
		if err != nil {
			return nil, err
		}
		if !seen[number] {
			numbers = append(numbers, number)
			seen[number] = true
		}
	}
	return u.repo.FindByNumbers(ctx, numbers)
}

func (u *CertificateUsecase) Records(ctx context.Context, input string, limit, offset int) ([]entity.VerificationRecord, int, error) {
	number, err := entity.NormalizeCertificateReference(input)
	if err != nil {
		return nil, 0, err
	}
	// Validate existence even when the selected page is empty.
	if _, err = u.repo.FindByNumber(ctx, number); err != nil {
		return nil, 0, err
	}
	return u.repo.Records(ctx, number, limit, offset)
}

func (u *CertificateUsecase) CurrentByIMEI(ctx context.Context, input string) (*entity.Certificate, error) {
	imei, err := entity.NormalizeIMEI(input)
	if err != nil {
		return nil, err
	}
	return u.repo.FindCurrentByIMEI(ctx, imei)
}

func (u *CertificateUsecase) Revoke(ctx context.Context, p entity.Principal, input string) error {
	if !validPrincipal(p) {
		return entity.ErrUnauthenticated
	}
	ref, err := entity.NormalizeCertificateReference(input)
	if err != nil {
		return err
	}
	return u.repo.RevokeOwned(ctx, p, ref)
}
