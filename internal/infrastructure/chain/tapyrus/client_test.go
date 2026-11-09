package tapyrus

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
)

var root = [32]byte{0xaa, 0xbb}
var rootHex = hex.EncodeToString(root[:])

const txid = "6fce02d39279f6d645ecc710ebcf1dbb7b8104106553d8da13f5db79c5a628fc"

func server(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := NewWithHTTPClient(srv.URL, "secret-token", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func record(status string, content string) map[string]any {
	r := map[string]any{"id": 7, "version": "2", "txid": txid, "status": status, "content_hash": content,
		"prefix": Prefix, "timestamp_type": "simple", "block_height": nil, "block_time": nil}
	if status == "confirmed" {
		r["block_height"], r["block_time"] = 101, 1626169080
	}
	return r
}

func TestSubmitSendsRawRootWithPrefix(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/timestamps" || r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]string
		_ = json.Unmarshal(body, &got)
		if got["type"] != "simple" || got["digest"] != "none" || got["prefix"] != Prefix || got["content"] != rootHex {
			t.Errorf("body %s", body)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(record("unconfirmed", rootHex))
	})
	r, err := c.Submit(context.Background(), root)
	if err != nil || r.Ref != "7" || *r.TxID != txid || r.Status != entity.TimestampUnconfirmed || r.Confirmed() {
		t.Fatalf("receipt %+v err %v", r, err)
	}
}

func TestFindReturnsBlockData(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/timestamps/7" {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(record("confirmed", rootHex))
	})
	r, err := c.Find(context.Background(), "7")
	if err != nil || !r.Confirmed() || *r.BlockHeight != 101 || r.BlockTime.Unix() != 1626169080 {
		t.Fatalf("receipt %+v err %v", r, err)
	}
	if _, err := c.Find(context.Background(), "../users"); !errors.Is(err, repository.ErrRejected) {
		t.Fatal("non-numeric id must be rejected before any request")
	}
}

func TestMismatchedContentIsRejected(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(record("unconfirmed", strings.Repeat("00", 32)))
	})
	if _, err := c.Submit(context.Background(), root); !errors.Is(err, repository.ErrRejected) {
		t.Fatalf("err %v", err)
	}
}

func TestFindByRootMatchesOnlyOurPrefix(t *testing.T) {
	other := record("confirmed", rootHex)
	other["prefix"] = "00"
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"count": 2, "timestamps": []any{other, record("init", rootHex)}})
	})
	r, err := c.FindByRoot(context.Background(), root)
	if err != nil || r == nil || r.Status != entity.TimestampInit {
		t.Fatalf("receipt %+v err %v", r, err)
	}
	r, err = c.FindByRoot(context.Background(), [32]byte{1})
	if err != nil || r != nil {
		t.Fatalf("unknown root: %+v %v", r, err)
	}
}

func TestStatusClassificationAndNoTokenLeak(t *testing.T) {
	for status, rejected := range map[int]bool{400: true, 404: true, 422: true, 401: false, 429: false, 503: false} {
		c := server(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"status":1,"error":"bad"}`))
		})
		_, err := c.Submit(context.Background(), root)
		if err == nil || errors.Is(err, repository.ErrRejected) != rejected || strings.Contains(err.Error(), "secret-token") {
			t.Fatalf("status %d: %v", status, err)
		}
	}
}

func TestConfigValidation(t *testing.T) {
	for _, u := range []string{"http://example.com", "https://user@x.example", "https://x.example?q=1", "notaurl"} {
		if _, err := NewWithHTTPClient(u, "t", http.DefaultClient); err == nil {
			t.Fatalf("%s must be rejected", u)
		}
	}
	if _, err := NewWithHTTPClient("https://x.example", " ", http.DefaultClient); err == nil {
		t.Fatal("empty token must be rejected")
	}
	if _, err := New("https://x.example", "t", "/nonexistent.pem", 0); err == nil {
		t.Fatal("missing certificate must fail")
	}
}
