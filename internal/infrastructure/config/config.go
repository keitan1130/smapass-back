package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr               string
	DBDSN                  string
	CORSOrigins            []string
	AttestationPackage     string
	AttestationDigests     []string
	AttestationRequireIMEI bool
}

// Load reads process environment. Compose loads .env; binaries do not.
func Load() (Config, error) {
	c := Config{HTTPAddr: os.Getenv("HTTP_ADDR"), DBDSN: os.Getenv("DB_DSN")}
	c.AttestationPackage = strings.TrimSpace(os.Getenv("ATTESTATION_PACKAGE_NAME"))
	for _, digest := range strings.Split(os.Getenv("ATTESTATION_SIGNING_SHA256"), ",") {
		if digest = strings.TrimSpace(digest); digest != "" {
			c.AttestationDigests = append(c.AttestationDigests, digest)
		}
	}
	if value := os.Getenv("ATTESTATION_REQUIRE_IMEI"); value != "" {
		flag, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("ATTESTATION_REQUIRE_IMEI must be true or false")
		}
		c.AttestationRequireIMEI = flag
	}
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

// AnchorConfig is read only by the anchor worker (cmd/anchor). The API server does not need
// Tapyrus credentials, and the worker refuses to start without them.
type AnchorConfig struct {
	DBDSN              string
	TapyrusURL         string
	TapyrusToken       string
	TapyrusCertFile    string
	Chain              string
	BatchInterval      time.Duration
	PollInterval       time.Duration
	MaxLeaves          int
	TapyrusHTTPTimeout time.Duration
}

func LoadAnchor() (AnchorConfig, error) {
	c := AnchorConfig{
		DBDSN:              os.Getenv("DB_DSN"),
		TapyrusURL:         strings.TrimSpace(os.Getenv("TAPYRUS_API_URL")),
		TapyrusToken:       strings.TrimSpace(os.Getenv("TAPYRUS_ACCESS_TOKEN")),
		TapyrusCertFile:    strings.TrimSpace(os.Getenv("TAPYRUS_CLIENT_CERT_FILE")),
		Chain:              strings.TrimSpace(os.Getenv("ANCHOR_CHAIN")),
		TapyrusHTTPTimeout: 30 * time.Second,
	}
	if c.DBDSN == "" {
		return AnchorConfig{}, fmt.Errorf("DB_DSN is required")
	}
	if c.TapyrusURL == "" || c.TapyrusToken == "" || c.TapyrusCertFile == "" {
		return AnchorConfig{}, fmt.Errorf("TAPYRUS_API_URL, TAPYRUS_ACCESS_TOKEN and TAPYRUS_CLIENT_CERT_FILE are required")
	}
	if c.Chain == "" {
		c.Chain = "tapyrus"
	}
	var err error
	if c.BatchInterval, err = duration("ANCHOR_BATCH_INTERVAL", 10*time.Minute); err != nil {
		return AnchorConfig{}, err
	}
	if c.PollInterval, err = duration("ANCHOR_POLL_INTERVAL", 30*time.Second); err != nil {
		return AnchorConfig{}, err
	}
	c.MaxLeaves = 1000
	if value := os.Getenv("ANCHOR_MAX_LEAVES"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 100000 {
			return AnchorConfig{}, fmt.Errorf("ANCHOR_MAX_LEAVES must be between 1 and 100000")
		}
		c.MaxLeaves = n
	}
	return c, nil
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < time.Second {
		return 0, fmt.Errorf("%s must be a duration of at least 1s, such as 30s or 10m", name)
	}
	return d, nil
}
