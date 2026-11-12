package di

import (
	"context"

	"smapass-back/internal/infrastructure/chain/tapyrus"
	"smapass-back/internal/infrastructure/config"
	anchordatabase "smapass-back/internal/infrastructure/database/anchor"
	"smapass-back/internal/usecase"
	"smapass-back/pkg/db"
)

// InitializeAnchorWorker wires the Tapyrus anchor worker. It runs as its own process (cmd/anchor)
// so the API server never holds chain credentials.
func InitializeAnchorWorker(ctx context.Context, cfg config.AnchorConfig) (*usecase.AnchorUsecase, func(), error) {
	client, err := tapyrus.New(cfg.TapyrusURL, cfg.TapyrusToken, cfg.TapyrusCertFile, cfg.TapyrusHTTPTimeout)
	if err != nil {
		return nil, nil, err
	}
	database, err := db.Open(ctx, cfg.DBDSN)
	if err != nil {
		return nil, nil, err
	}
	u := usecase.NewAnchorUsecase(anchordatabase.NewRepository(database), client, usecase.AnchorOptions{
		Chain: cfg.Chain, MaxLeaves: cfg.MaxLeaves, PollInterval: cfg.PollInterval,
	})
	return u, func() { _ = database.Close() }, nil
}
