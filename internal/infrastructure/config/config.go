package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr      string
	DBDSN         string
	CORSOrigins   []string
	AuthPublicKey string
	AuthIssuer    string
	AuthAudience  string
}

// Load reads process environment. Compose loads .env; binaries do not.
func Load() (Config, error) {
	c := Config{HTTPAddr: os.Getenv("HTTP_ADDR"), DBDSN: os.Getenv("DB_DSN")}
	c.AuthPublicKey, c.AuthIssuer, c.AuthAudience = os.Getenv("AUTH_PUBLIC_KEY"), os.Getenv("AUTH_ISSUER"), os.Getenv("AUTH_AUDIENCE")
	if c.HTTPAddr == "" {
		c.HTTPAddr = ":8080"
	}
	if c.DBDSN == "" {
		return Config{}, fmt.Errorf("DB_DSN is required")
	}
	for _, origin := range strings.Split(os.Getenv("CORS_ORIGINS"), ",") {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return Config{}, fmt.Errorf("CORS_ORIGINS must contain explicit http(s) origins")
		}
		c.CORSOrigins = append(c.CORSOrigins, origin)
	}
	return c, nil
}
