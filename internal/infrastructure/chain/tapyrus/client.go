// Package tapyrus records Merkle roots with the Chaintope Tapyrus API (timestamp v2).
//
// Each root is sent as type "simple" with digest "none", so the transaction carries
//
//	OP_RETURN <Prefix><32-byte merkle root>
//
// and a browser can compare the OP_RETURN data with the merkle_root it recomputes.
package tapyrus

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
)

// Prefix is ASCII "smapass" followed by format version 0x01 (smapass-merkle-v1).
const Prefix = "736d617061737301"

const maxResponseBytes = 1 << 20

type Client struct {
	base  *url.URL
	token string
	http  *http.Client
}

var _ repository.TimestampClient = (*Client)(nil)

// New builds a client using the PEM file produced by
// `openssl pkcs12 -in <file>.p12 -out cert.pem -nodes -clcerts` (certificate and key in one file).
func New(baseURL, token, certFile string, timeout time.Duration) (*Client, error) {
	cert, err := tls.LoadX509KeyPair(certFile, certFile)
	if err != nil {
		// The path is configuration, not a secret; the file content is never printed.
		return nil, fmt.Errorf("load Tapyrus client certificate %s: %w", certFile, err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	return NewWithHTTPClient(baseURL, token, &http.Client{Timeout: timeout, Transport: transport})
}

func NewWithHTTPClient(baseURL, token string, httpClient *http.Client) (*Client, error) {
	base, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("TAPYRUS_API_URL must be an origin such as https://xxx.api.tapyrus.chaintope.com")
	}
	if base.Scheme != "https" && !(base.Scheme == "http" && isLoopback(base.Hostname())) {
		return nil, fmt.Errorf("TAPYRUS_API_URL must use https")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("TAPYRUS_ACCESS_TOKEN is required")
	}
	return &Client{base: base, token: token, http: httpClient}, nil
}

func isLoopback(host string) bool { return host == "127.0.0.1" || host == "localhost" || host == "::1" }

type timestamp struct {
	ID            int64   `json:"id"`
	TxID          *string `json:"txid"`
	Status        string  `json:"status"`
	ContentHash   string  `json:"content_hash"`
	Prefix        string  `json:"prefix"`
	TimestampType string  `json:"timestamp_type"`
	BlockHeight   *int64  `json:"block_height"`
	BlockTime     *int64  `json:"block_time"`
}

func (c *Client) Submit(ctx context.Context, root [32]byte) (*entity.TimestampReceipt, error) {
	body, _ := json.Marshal(map[string]string{
		"type": "simple", "digest": "none", "prefix": Prefix, "content": hex.EncodeToString(root[:]),
	})
	var ts timestamp
	if err := c.do(ctx, http.MethodPost, "/api/v2/timestamps", body, http.StatusCreated, &ts); err != nil {
		return nil, err
	}
	return receipt(ts, root)
}

func (c *Client) Find(ctx context.Context, ref string) (*entity.TimestampReceipt, error) {
	if _, err := strconv.ParseInt(ref, 10, 64); err != nil {
		return nil, fmt.Errorf("invalid timestamp id: %w", repository.ErrRejected)
	}
	var ts timestamp
	if err := c.do(ctx, http.MethodGet, "/api/v2/timestamps/"+ref, nil, http.StatusOK, &ts); err != nil {
		return nil, err
	}
	return receiptAny(ts)
}

func (c *Client) FindByRoot(ctx context.Context, root [32]byte) (*entity.TimestampReceipt, error) {
	var list struct {
		Timestamps []timestamp `json:"timestamps"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v2/timestamps", nil, http.StatusOK, &list); err != nil {
		return nil, err
	}
	want := hex.EncodeToString(root[:])
	for _, ts := range list.Timestamps {
		if strings.EqualFold(ts.ContentHash, want) && strings.EqualFold(ts.Prefix, Prefix) && ts.TimestampType == "simple" {
			return receipt(ts, root)
		}
	}
	return nil, nil
}

// receipt checks that the provider recorded exactly our prefix and root before trusting it.
func receipt(ts timestamp, root [32]byte) (*entity.TimestampReceipt, error) {
	if !strings.EqualFold(ts.ContentHash, hex.EncodeToString(root[:])) || !strings.EqualFold(ts.Prefix, Prefix) {
		return nil, fmt.Errorf("provider recorded different content: %w", repository.ErrRejected)
	}
	return receiptAny(ts)
}

func receiptAny(ts timestamp) (*entity.TimestampReceipt, error) {
	if ts.ID <= 0 || ts.TimestampType != "simple" || !strings.EqualFold(ts.Prefix, Prefix) {
		return nil, fmt.Errorf("unexpected timestamp record: %w", repository.ErrRejected)
	}
	switch ts.Status {
	case entity.TimestampInit, entity.TimestampUnconfirmed, entity.TimestampConfirmed:
	default:
		return nil, fmt.Errorf("unknown timestamp status %q", ts.Status)
	}
	if ts.TxID != nil {
		if b, err := hex.DecodeString(*ts.TxID); err != nil || len(b) != 32 {
			return nil, fmt.Errorf("invalid txid from provider")
		}
	}
	r := &entity.TimestampReceipt{Ref: strconv.FormatInt(ts.ID, 10), TxID: ts.TxID, Status: ts.Status, BlockHeight: ts.BlockHeight}
	if ts.BlockTime != nil {
		t := time.Unix(*ts.BlockTime, 0).UTC()
		r.BlockTime = &t
	}
	return r, nil
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, want int, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base.JoinPath(path).String(), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			// url.Error repeats the URL; keep only the cause.
			return fmt.Errorf("tapyrus %s %s: %w", method, path, urlErr.Err)
		}
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	if res.StatusCode != want {
		return statusError(method, path, res.StatusCode, data)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("tapyrus %s %s: invalid JSON response", method, path)
	}
	return nil
}

func statusError(method, path string, status int, data []byte) error {
	var apiErr struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &apiErr)
	msg := apiErr.Error
	if len(msg) > 200 {
		msg = msg[:200]
	}
	err := fmt.Errorf("tapyrus %s %s: HTTP %d %s", method, path, status, msg)
	switch status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity:
		// The request itself is wrong; retrying the same payload cannot succeed.
		return fmt.Errorf("%w: %w", err, repository.ErrRejected)
	default:
		// 401/403 (credentials), 429 and 5xx are retried with backoff.
		return err
	}
}
