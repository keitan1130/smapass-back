package config

import "testing"

func TestTrustedProxyConfig(t *testing.T) {
	t.Setenv("DB_DSN", "postgres://example")
	t.Setenv("CORS_ORIGINS", "")
	t.Setenv("ATTESTATION_REQUIRE_IMEI", "false")
	for _, tt := range []struct {
		value   string
		count   int
		invalid bool
	}{
		{"", 0, false},
		{"172.30.50.2/32, fd00::2/128", 2, false},
		{"172.30.50.2", 0, true},
		{"0.0.0.0/0", 0, true},
		{"::/0", 0, true},
		{"::ffff:172.30.50.2/128", 0, true},
		{"172.30.50.2/32,", 0, true},
	} {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXY_CIDRS", tt.value)
			cfg, err := Load()
			if (err != nil) != tt.invalid {
				t.Fatalf("error=%v invalid=%v", err, tt.invalid)
			}
			if err == nil && len(cfg.TrustedProxyCIDRs) != tt.count {
				t.Fatalf("unexpected prefixes: %v", cfg.TrustedProxyCIDRs)
			}
		})
	}
}
