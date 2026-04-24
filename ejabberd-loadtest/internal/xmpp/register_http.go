package xmpp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPRegistrar calls ejabberd's mod_http_api /api/register endpoint.
// Endpoint typically looks like: https://host:5443/api/register
// It accepts: {"user": "...", "host": "...", "password": "..."}
type HTTPRegistrar struct {
	Endpoint   string
	Domain     string // host field for the XMPP domain
	AdminUser  string // optional HTTP basic auth user (bare or full JID)
	AdminPass  string // optional HTTP basic auth password
	Insecure   bool   // skip TLS certificate verification
	Timeout    time.Duration
	httpClient *http.Client
}

func NewHTTPRegistrar(endpoint, domain string, insecure bool) *HTTPRegistrar {
	return &HTTPRegistrar{
		Endpoint: endpoint,
		Domain:   domain,
		Insecure: insecure,
		Timeout:  10 * time.Second,
	}
}

func (r *HTTPRegistrar) client() *http.Client {
	if r.httpClient != nil {
		return r.httpClient
	}
	tr := &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: r.Insecure},
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	r.httpClient = &http.Client{Transport: tr, Timeout: r.Timeout}
	return r.httpClient
}

// Register creates a new user via the HTTP API. Returns nil on 200/201,
// or nil if the user already exists (which is treated as success for
// load-testing purposes).
func (r *HTTPRegistrar) Register(ctx context.Context, user, pass string) error {
	payload := map[string]string{
		"user":     user,
		"host":     r.Domain,
		"password": pass,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if r.AdminUser != "" {
		req.SetBasicAuth(r.AdminUser, r.AdminPass)
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	// ejabberd returns 409 / "already exists" when user exists. Treat as ok.
	if resp.StatusCode == http.StatusConflict ||
		bytes.Contains(bytes.ToLower(body), []byte("already")) ||
		bytes.Contains(bytes.ToLower(body), []byte("exists")) {
		return nil
	}
	return fmt.Errorf("http register: status=%d body=%s", resp.StatusCode, string(body))
}
