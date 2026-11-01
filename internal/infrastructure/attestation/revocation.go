package attestation

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"smapass-back/internal/domain/entity"
)

const statusURL = "https://android.googleapis.com/attestation/status"

type GoogleStatus struct {
	client  *http.Client
	mu      sync.Mutex
	expires time.Time
	entries map[string]json.RawMessage
}

func NewGoogleStatus() *GoogleStatus {
	return &GoogleStatus{client: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (s *GoogleStatus) Check(ctx context.Context, chain []*x509.Certificate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil || !time.Now().Before(s.expires) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
		if err != nil {
			return entity.ErrAttestationUnavailable
		}
		response, err := s.client.Do(request)
		if err != nil {
			return entity.ErrAttestationUnavailable
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return entity.ErrAttestationUnavailable
		}
		const limit = 4 << 20
		data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
		if err != nil || len(data) > limit {
			return entity.ErrAttestationUnavailable
		}
		var list struct {
			Entries map[string]json.RawMessage `json:"entries"`
		}
		if json.Unmarshal(data, &list) != nil || list.Entries == nil {
			return entity.ErrAttestationUnavailable
		}
		// Unknown or malformed statuses never become implicit "good" statuses.
		for _, raw := range list.Entries {
			var entry struct {
				Status string `json:"status"`
			}
			if json.Unmarshal(raw, &entry) != nil || (entry.Status != "REVOKED" && entry.Status != "SUSPENDED") {
				return entity.ErrAttestationUnavailable
			}
		}
		s.entries = list.Entries
		ttl := time.Duration(0)
		maxAge, noCache := 0, false
		for _, directive := range strings.Split(response.Header.Get("Cache-Control"), ",") {
			name, value, ok := strings.Cut(strings.TrimSpace(directive), "=")
			if strings.EqualFold(name, "no-store") || strings.EqualFold(name, "no-cache") {
				noCache = true
			}
			if ok && strings.EqualFold(name, "max-age") {
				if seconds, err := strconv.Atoi(strings.Trim(value, `"`)); err == nil && seconds > 0 {
					maxAge = min(seconds, 86400)
				}
			}
		}
		age, _ := strconv.Atoi(response.Header.Get("Age"))
		age = max(0, age)
		if maxAge > 0 && age >= maxAge {
			return entity.ErrAttestationUnavailable
		}
		if !noCache {
			ttl = time.Duration(min(3600, max(0, maxAge-age))) * time.Second
		}

		s.expires = time.Now().Add(ttl)
	}
	for _, cert := range chain {
		if _, revoked := s.entries[cert.SerialNumber.Text(16)]; revoked {
			return entity.ErrAttestationRejected
		}
	}
	return nil
}
