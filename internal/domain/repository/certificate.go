package repository

import (
	"context"
	"errors"

	"smapass-back/internal/domain/entity"
)

var ErrNotFound = errors.New("certificate not found")

//go:generate go tool mockgen -source=certificate.go -destination=mock/certificate_mock.go -package=mock -self_package=smapass-back/internal/domain/repository/mock -write_command_comment=false
type CertificateRepository interface {
	FindCurrentByIMEI(context.Context, string) (*entity.Certificate, error)
	FindByNumber(context.Context, string) (*entity.Certificate, error)
	FindByNumbers(context.Context, []string) ([]entity.Certificate, error)
	Records(context.Context, string, int, int) ([]entity.VerificationRecord, int, error)
}
