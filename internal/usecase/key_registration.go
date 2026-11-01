package usecase

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
)

type KeyRegistrationUsecase struct {
	challenges repository.KeyChallengeRepository
	verifier   repository.KeyAttestationVerifier
}

func NewKeyRegistrationUsecase(challenges repository.KeyChallengeRepository, verifier repository.KeyAttestationVerifier) *KeyRegistrationUsecase {
	return &KeyRegistrationUsecase{challenges: challenges, verifier: verifier}
}

func (u *KeyRegistrationUsecase) ready() error {
	if u == nil || u.verifier == nil || !u.verifier.Ready() {
		return entity.ErrAttestationUnavailable
	}
	return nil
}
func (u *KeyRegistrationUsecase) EnrollmentChallenge(ctx context.Context, input entity.EnrollmentChallengeRequest) (*entity.KeyChallenge, error) {
	if err := u.ready(); err != nil {
		return nil, err
	}
	imei, err := entity.NormalizeIMEI(input.IMEI)
	if err != nil {
		return nil, err
	}
	return u.challenges.Create(ctx, "enroll", []string{imei}, entity.Principal{})
}
func (u *KeyRegistrationUsecase) RotationChallenge(ctx context.Context, p entity.Principal, input entity.RotationChallengeRequest) (*entity.KeyChallenge, error) {
	if !validPrincipal(p) {
		return nil, entity.ErrUnauthenticated
	}
	if err := u.ready(); err != nil {
		return nil, err
	}
	imeis, err := entity.NormalizeIMEIs(input.IMEIs)
	if err != nil {
		return nil, err
	}
	return u.challenges.Create(ctx, "rotate", imeis, p)
}

func (u *KeyRegistrationUsecase) Verify(ctx context.Context, purpose string, p entity.Principal, input *entity.RegisterDevice) error {
	if err := u.ready(); err != nil {
		return err
	}
	if err := input.Normalize(); err != nil {
		return err
	}
	id, err := uuid.Parse(input.Proof.ChallengeID)
	if err != nil || id.String() != input.Proof.ChallengeID {
		return entity.ErrChallengeInvalid
	}
	challenge, err := u.challenges.Find(ctx, id.String())
	if err != nil {
		return err
	}
	requested, err := entity.NormalizeIMEIs(input.IMEIs)
	if err != nil {
		return err
	}
	if !time.Now().Before(challenge.ExpiresAt) || purpose != challenge.Purpose || !slices.Equal(requested, challenge.IMEIs) {
		return entity.ErrChallengeInvalid
	}
	if purpose == "enroll" {
		if challenge.ConsumedAt != nil || challenge.InstallationID != "" {
			return entity.ErrChallengeInvalid
		}
	} else if !validPrincipal(p) || challenge.InstallationID != p.Subject || challenge.IMEI != p.IMEI {
		return entity.ErrChallengeInvalid
	}
	verified, err := u.verifier.Verify(ctx, input.PublicKey, input.Proof, challenge)
	if err != nil {
		return err
	}
	input.Verified = verified
	return nil
}
