package updatecheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes int64 = 32 << 10

type Client struct {
	httpClient *http.Client
	userAgent  string
}

func NewClient(userAgent string) *Client {
	return &Client{
		userAgent: strings.TrimSpace(userAgent),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// NewClientWithHTTPClient is intended for tests and controlled integrations.
func NewClientWithHTTPClient(userAgent string, httpClient *http.Client) *Client {
	client := NewClient(userAgent)
	if httpClient != nil {
		client.httpClient = httpClient
	}
	return client
}

func (c *Client) Check(ctx context.Context, endpoint string, payload Request) (Response, error) {
	if c == nil || c.httpClient == nil {
		return Response{}, fmt.Errorf("update client is unavailable")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return Response{}, fmt.Errorf("invalid update endpoint")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return Response{}, fmt.Errorf("update endpoint must use HTTPS")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("encode update request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("create update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("send update request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("update endpoint returned %s", resp.Status)
	}
	limited := io.LimitReader(resp.Body, maxResponseBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return Response{}, fmt.Errorf("read update response: %w", err)
	}
	if int64(len(raw)) > maxResponseBytes {
		return Response{}, fmt.Errorf("update response is too large")
	}
	var result Response
	if err := json.Unmarshal(raw, &result); err != nil {
		return Response{}, fmt.Errorf("decode update response: %w", err)
	}
	if result.ProtocolVersion != ProtocolVersion {
		return Response{}, fmt.Errorf("unsupported update protocol version %d", result.ProtocolVersion)
	}
	if len(result.Updates) > 100 {
		return Response{}, fmt.Errorf("update response contains too many targets")
	}
	return result, nil
}
