package repository

import (
	"context"
	"errors"

	"smapass-back/internal/domain/entity"
)

var ErrNotFound = errors.New("certificate not found")

type CertificateRepository interface {
	FindByNumber(context.Context, string) (*entity.Certificate, error)
	FindByNumbers(context.Context, []string) ([]entity.Certificate, error)
	Records(context.Context, string, int, int) ([]entity.VerificationRecord, int, error)
}
