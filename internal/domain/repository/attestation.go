package repository

import (
	"context"
	"smapass-back/internal/domain/entity"
)

type KeyChallengeRepository interface {
	Create(context.Context, string, []string, entity.Principal) (*entity.KeyChallenge, error)
	Find(context.Context, string) (*entity.ChallengeContext, error)
}

type KeyAttestationVerifier interface {
	Ready() bool
	Verify(context.Context, string, entity.KeyProof, *entity.ChallengeContext) (*entity.VerifiedKey, error)
}
