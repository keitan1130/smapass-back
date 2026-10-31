package auth

import (
	"crypto/rsa"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"smapass-back/internal/domain/entity"
	requestauth "smapass-back/internal/interface/auth"
)

// Verifier accepts tokens from the configured issuer. Token issuance belongs to #1.
type Verifier struct {
	key              *rsa.PublicKey
	issuer, audience string
}

func NewVerifier(publicKey, issuer, audience string) (*Verifier, error) {
	if publicKey == "" && issuer == "" && audience == "" {
		return nil, nil
	}
	if publicKey == "" || issuer == "" || audience == "" {
		return nil, fmt.Errorf("AUTH_PUBLIC_KEY, AUTH_ISSUER and AUTH_AUDIENCE must be configured together")
	}
	key, err := jwt.ParseRSAPublicKeyFromPEM([]byte(publicKey))
	if err != nil || key.N.BitLen() < 2048 {
		return nil, fmt.Errorf("AUTH_PUBLIC_KEY must be a PEM RSA public key of at least 2048 bits")
	}
	return &Verifier{key: key, issuer: issuer, audience: audience}, nil
}

func (v *Verifier) Middleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if v == nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "authentication is not configured")
		}
		parts := strings.Fields(c.Request().Header.Get(echo.HeaderAuthorization))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 8192 {
			return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
		}
		claims := &jwt.RegisteredClaims{}
		_, err := jwt.ParseWithClaims(parts[1], claims, func(_ *jwt.Token) (any, error) { return v.key, nil },
			jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
		if err != nil || strings.TrimSpace(claims.Subject) == "" || len(claims.Subject) > 255 {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid access token")
		}
		ctx := requestauth.WithPrincipal(c.Request().Context(), entity.Principal{Issuer: claims.Issuer, Subject: claims.Subject})
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}
