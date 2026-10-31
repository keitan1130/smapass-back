package usecase

import "context"

type DatabaseHealth interface{ PingContext(context.Context) error }

type HealthUsecase struct{ database DatabaseHealth }

func NewHealthUsecase(database DatabaseHealth) *HealthUsecase {
	return &HealthUsecase{database: database}
}

func (u *HealthUsecase) Ready(ctx context.Context) error { return u.database.PingContext(ctx) }
