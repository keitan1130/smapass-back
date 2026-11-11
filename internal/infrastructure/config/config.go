package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr               string
	DBDSN                  string
	CORSOrigins            []string
	TrustedProxyCIDRs      []netip.Prefix
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
	if value := strings.TrimSpace(os.Getenv("TRUSTED_PROXY_CIDRS")); value != "" {
		for _, item := range strings.Split(value, ",") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(item))
			if err != nil || prefix.Bits() == 0 || prefix.Addr().Is4In6() {
				return Config{}, fmt.Errorf("TRUSTED_PROXY_CIDRS must contain explicit IPv4 or IPv6 CIDRs, not all-address ranges")
			}
			c.TrustedProxyCIDRs = append(c.TrustedProxyCIDRs, prefix.Masked())
		}
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
