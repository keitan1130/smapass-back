package di

import (
	"context"

	"github.com/labstack/echo/v5"
	"smapass-back/internal/infrastructure/config"
	"smapass-back/internal/infrastructure/database/certificate"
	"smapass-back/internal/infrastructure/router"
	"smapass-back/internal/interface/controller"
	"smapass-back/internal/usecase"
	"smapass-back/pkg/db"
)

// InitializeApp wires constructors explicitly. No generated DI code is required.
func InitializeApp(ctx context.Context, cfg config.Config) (*echo.Echo, func(), error) {
	database, err := db.Open(ctx, cfg.DBDSN)
	if err != nil {
		return nil, nil, err
	}
	repo := certificate.NewRepository(database)
	certificates := controller.NewCertificateController(usecase.NewCertificateUsecase(repo))
	health := controller.NewHealthController(usecase.NewHealthUsecase(database))
	app := router.New(certificates, health, cfg.CORSOrigins)
	return app, func() { _ = database.Close() }, nil
}
