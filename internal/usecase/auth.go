package usecase

import (
	"context"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
)

type AuthUsecase struct{ repo repository.AuthRepository }

func NewAuthUsecase(repo repository.AuthRepository) *AuthUsecase { return &AuthUsecase{repo: repo} }

func (u *AuthUsecase) Enroll(ctx context.Context, input entity.EnrollDevice) (*entity.TokenPair, error) {
	imei, err := entity.NormalizeIMEI(input.IMEI)
	if err != nil {
		return nil, err
	}
	return u.repo.Enroll(ctx, imei, entity.NewTokenMaterial())
}
func (u *AuthUsecase) Refresh(ctx context.Context, input entity.RefreshRequest) (*entity.TokenPair, error) {
	if !entity.ValidToken(input.RefreshToken, "rt") {
		return nil, entity.ErrUnauthenticated
	}
	return u.repo.Refresh(ctx, input.RefreshToken, entity.NewTokenMaterial())
}
func (u *AuthUsecase) Revoke(ctx context.Context, input entity.RefreshRequest) error {
	// Unknown credentials are an idempotent no-op; never disclose whether they exist.
	if !entity.ValidToken(input.RefreshToken, "rt") {
		return nil
	}
	return u.repo.Revoke(ctx, input.RefreshToken)
}
func (u *AuthUsecase) Authenticate(ctx context.Context, token string) (entity.Principal, error) {
	if !entity.ValidToken(token, "at") {
		return entity.Principal{}, entity.ErrUnauthenticated
	}
	return u.repo.Authenticate(ctx, token)
}
