package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
)

type AuthUsecase struct {
	repo repository.AuthRepository
	keys *KeyRegistrationUsecase
}

func NewAuthUsecase(repo repository.AuthRepository, keys *KeyRegistrationUsecase) *AuthUsecase {
	return &AuthUsecase{repo: repo, keys: keys}
}

func (u *AuthUsecase) Enroll(ctx context.Context, input entity.EnrollDevice) (*entity.TokenPair, error) {
	imei, err := entity.NormalizeIMEI(input.IMEI)
	if err != nil {
		return nil, err
	}
	if err := u.keys.ready(); err != nil {
		return nil, err
	}
	public, err := entity.NormalizePublicKey(input.PublicKey)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(input.Proof.ChallengeID)
	if err != nil || id.String() != input.Proof.ChallengeID {
		return nil, entity.ErrChallengeInvalid
	}
	challenge, err := u.keys.challenges.Find(ctx, id.String())
	if err != nil {
		return nil, err
	}
	if !time.Now().Before(challenge.ExpiresAt) || challenge.Purpose != "enroll" || challenge.IMEI != imei || challenge.ConsumedAt != nil || challenge.InstallationID != "" {
		return nil, entity.ErrChallengeInvalid
	}
	verified, err := u.keys.verifier.Verify(ctx, public, input.Proof, challenge)
	if err != nil {
		return nil, err
	}
	input.IMEI = imei
	input.PublicKey = public
	input.Verified = verified
	return u.repo.Enroll(ctx, input, entity.NewTokenMaterial())
}

func (u *AuthUsecase) Challenge(ctx context.Context, input entity.EnrollmentChallengeRequest) (*entity.KeyChallenge, error) {
	return u.keys.EnrollmentChallenge(ctx, input)
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
