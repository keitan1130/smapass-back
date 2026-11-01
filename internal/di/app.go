package di

import (
	"context"

	"github.com/labstack/echo/v5"
	"smapass-back/internal/infrastructure/attestation"
	"smapass-back/internal/infrastructure/auth"
	"smapass-back/internal/infrastructure/config"
	authdatabase "smapass-back/internal/infrastructure/database/auth"
	"smapass-back/internal/infrastructure/database/certificate"
	"smapass-back/internal/infrastructure/database/device"
	"smapass-back/internal/infrastructure/database/keychallenge"
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
	verifier, err := attestation.NewVerifier(attestation.Policy{PackageName: cfg.AttestationPackage, SigningDigests: cfg.AttestationDigests, RequireIMEI: cfg.AttestationRequireIMEI}, attestation.NewGoogleStatus())
	if err != nil {
		_ = database.Close()
		return nil, nil, err
	}
	keys := usecase.NewKeyRegistrationUsecase(keychallenge.NewRepository(database), verifier)
	authUsecase := usecase.NewAuthUsecase(authdatabase.NewRepository(database), keys)
	authenticate := auth.Middleware(authUsecase.Authenticate)
	router.AddDeviceRoutes(app, controller.NewDeviceController(usecase.NewDeviceUsecase(device.NewRepository(database), keys)), authenticate)
	router.AddAuthRoutes(app, controller.NewAuthController(authUsecase), certificates, authenticate)
	return app, func() { _ = database.Close() }, nil
}
