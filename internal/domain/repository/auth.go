package repository

import (
	"context"

	"smapass-back/internal/domain/entity"
)

type AuthRepository interface {
	Enroll(context.Context, entity.EnrollDevice, entity.TokenMaterial) (*entity.TokenPair, error)
	Refresh(context.Context, string, entity.TokenMaterial) (*entity.TokenPair, error)
	Revoke(context.Context, string) error
	Authenticate(context.Context, string) (entity.Principal, error)
}
