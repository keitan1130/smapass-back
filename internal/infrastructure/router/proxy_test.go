package router

import (
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func TestCloudflareIPExtractor(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("172.30.50.2/32"), netip.MustParsePrefix("fd00::2/128")}
	for _, tt := range []struct {
		name, peer string
		headers    []string
		want       string
	}{
		{"trusted", "172.30.50.2:1234", []string{"203.0.113.1"}, "203.0.113.1"},
		{"untrusted spoof", "172.30.50.3:1234", []string{"203.0.113.1"}, "172.30.50.3"},
		{"missing", "172.30.50.2:1234", nil, "172.30.50.2"},
		{"invalid", "172.30.50.2:1234", []string{"invalid"}, "172.30.50.2"},
		{"list", "172.30.50.2:1234", []string{"203.0.113.1, 203.0.113.2"}, "172.30.50.2"},
		{"duplicate", "172.30.50.2:1234", []string{"203.0.113.1", "203.0.113.2"}, "172.30.50.2"},
		{"IPv6", "[fd00::2]:1234", []string{"2001:db8::1"}, "2001:db8::1"},
		{"mapped peer and client", "[::ffff:172.30.50.2]:1234", []string{"::ffff:203.0.113.1"}, "203.0.113.1"},
		{"zone", "172.30.50.2:1234", []string{"fe80::1%eth0"}, "172.30.50.2"},
		{"unspecified", "172.30.50.2:1234", []string{"0.0.0.0"}, "172.30.50.2"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.peer
			for _, value := range tt.headers {
				r.Header.Add("CF-Connecting-IP", value)
			}
			r.Header.Set("X-Forwarded-For", "192.0.2.99")
			r.Header.Set("X-Real-IP", "192.0.2.99")
			if got := CloudflareIPExtractor(trusted)(r); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if got := CloudflareIPExtractor(nil)(r); got != echo.ExtractIPDirect()(r) {
				t.Fatalf("unconfigured extractor trusted headers: %q", got)
			}
		})
	}
}

func TestCloudflareRateLimitIsolation(t *testing.T) {
	for _, trusted := range []bool{true, false} {
		app := echo.New()
		if trusted {
			app.IPExtractor = CloudflareIPExtractor([]netip.Prefix{netip.MustParsePrefix("172.30.50.2/32")})
		} else {
			app.IPExtractor = CloudflareIPExtractor(nil)
		}
		app.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{Rate: 0.0001, Burst: 1})))
		app.GET("/", func(c *echo.Context) error { return c.NoContent(204) })
		// Different claimed clients must share the same bucket when the peer is untrusted.
		for i, client := range []string{"203.0.113.1", "203.0.113.1", "203.0.113.2"} {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = "172.30.50.2:1234"
			r.Header.Set("CF-Connecting-IP", client)
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			want := 204
			if i == 1 || (i == 2 && !trusted) {
				want = 429
			}
			if w.Code != want {
				t.Fatalf("trusted=%v request=%d status=%d want=%d", trusted, i, w.Code, want)
			}
		}
	}
}
