package router

import (
	"net/http"
	"net/netip"
	"strings"

	"github.com/labstack/echo/v5"
)

// CloudflareIPExtractor accepts CF-Connecting-IP only from explicitly trusted
// direct peers. X-Forwarded-For and X-Real-IP are never used as fallbacks.
func CloudflareIPExtractor(trusted []netip.Prefix) echo.IPExtractor {
	direct := echo.ExtractIPDirect()
	return func(r *http.Request) string {
		peer := direct(r)
		addr, err := netip.ParseAddr(peer)
		if err != nil {
			return peer
		}
		allowed := false
		for _, prefix := range trusted {
			if prefix.Contains(addr.Unmap()) {
				allowed = true
				break
			}
		}
		if !allowed {
			return peer
		}
		values := r.Header.Values("CF-Connecting-IP")
		if len(values) != 1 {
			return peer
		}
		client, err := netip.ParseAddr(strings.TrimSpace(values[0]))
		if err != nil || client.Zone() != "" || client.IsUnspecified() || client.IsMulticast() {
			return peer
		}
		return client.Unmap().String()
	}
}
