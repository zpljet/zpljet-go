// Package zpljet provides the official Go client for the ZPLJet API.
package zpljet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Version is the SDK version, sent in the User-Agent header.
const Version = "1.0.0"

const (
	defaultBaseURL    = "https://api.zpljet.com"
	defaultTimeout    = 60 * time.Second
	defaultMaxRetries = 2
	maxRetriesCap     = 10
	baseRetryDelay    = 500 * time.Millisecond
	maxRetryDelay     = 30 * time.Second
)

// Format is the output file format.
type Format string

// Output formats accepted by ConvertRequest.Format.
const (
	// FormatPDF renders a PDF — the API default.
	FormatPDF Format = "pdf"
	// FormatPNG renders a PNG.
	FormatPNG Format = "png"
)

// ConvertRequest contains POST /v1/convert parameters. Zero values use API defaults.
type ConvertRequest struct {
	// ZPL is the raw label program — one or more ^XA…^XZ blocks. Must start
	// with ^XA (or ~DG) and end with ^XZ. Graphics must use uncompressed
	// ASCII ^GF/~DG data, up to 256 KB decoded. Max 512 KB total. Required.
	ZPL string `json:"zpl"`
	// DPMM is the print density in dots/mm: 6, 8 (default, 203 dpi),
	// 12 (300 dpi), or 24 (600 dpi).
	DPMM int `json:"dpmm,omitempty"`
	// WidthMm is the physical label width in millimeters (default 101.6, 4 in).
	WidthMm float64 `json:"widthMm,omitempty"`
	// HeightMm is the physical label height in millimeters (default 152.4, 6 in).
	HeightMm float64 `json:"heightMm,omitempty"`
	// Format selects PDF (default) or PNG output.
	Format Format `json:"format,omitempty"`
}

// LabelData is the result of Convert — the raw file bytes. Nothing is stored
// server-side.
type LabelData struct {
	// Data holds the rendered file bytes (PDF or PNG).
	Data []byte
	// ContentType is "application/pdf" or "image/png".
	ContentType string
	// ID is the conversion id (from the X-Conversion-Id response header).
	ID string
}

// HostedLabel is the result of ConvertToURL — the file is hosted by ZPLJet
// (paid plans) and served via a public link.
type HostedLabel struct {
	// ID is the conversion id.
	ID string `json:"id"`
	// URL is the public link to the hosted file. It works until the file is
	// deleted at ExpiresAt.
	URL string `json:"url"`
	// Pages is the number of pages rendered (one per ^XA…^XZ block).
	Pages int `json:"pages"`
	// RetentionDays is how many days the file is retained.
	RetentionDays int `json:"retentionDays"`
	// ExpiresAt is when the hosted file is deleted and its URL stops working (UTC).
	ExpiresAt time.Time `json:"expiresAt"`
}

// Client is safe for concurrent use and retries transient failures.
type Client struct {
	apiKey            string
	baseURL           string
	timeout           time.Duration
	maxRetries        int
	httpClient        *http.Client
	allowInsecureHTTP bool
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at a different API origin (e.g. a staging
// stack). Default: https://api.zpljet.com
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

// WithTimeout sets the per-attempt timeout. Default: 60s.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) { c.timeout = timeout }
}

// WithMaxRetries sets how many times a failed request is automatically
// retried (rate limits, transient 5xx, network errors). Default: 2. Use 0 to
// disable.
func WithMaxRetries(maxRetries int) Option {
	return func(c *Client) { c.maxRetries = maxRetries }
}

// WithHTTPClient supplies a custom *http.Client — useful for proxies,
// custom transports, and tests. Its Timeout should stay 0 (unlimited); the
// SDK enforces per-attempt timeouts itself.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) { c.httpClient = httpClient }
}

// WithAllowInsecureHTTP permits a plaintext http:// base URL to a non-loopback
// host. Off by default so the API key is never sent over an unencrypted
// connection by mistake; loopback hosts are always allowed over http.
func WithAllowInsecureHTTP() Option {
	return func(c *Client) { c.allowInsecureHTTP = true }
}

// New creates a Client for the given API key (zpl_…, created in the
// dashboard at https://zpljet.com/dashboard — keep it server-side).
func New(apiKey string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New(
			"zpljet: missing API key — create one at https://zpljet.com/dashboard")
	}
	client := &Client{
		apiKey:     strings.TrimSpace(apiKey),
		baseURL:    defaultBaseURL,
		timeout:    defaultTimeout,
		maxRetries: defaultMaxRetries,
		httpClient: &http.Client{},
	}
	for _, opt := range opts {
		opt(client)
	}
	if client.maxRetries < 0 {
		return nil, errors.New("zpljet: max retries must be >= 0")
	}
	if client.maxRetries > maxRetriesCap {
		client.maxRetries = maxRetriesCap
	}
	if client.timeout <= 0 {
		return nil, errors.New("zpljet: timeout must be greater than zero")
	}
	if client.httpClient == nil {
		return nil, errors.New("zpljet: HTTP client must not be nil")
	}
	if err := assertSecureBaseURL(client.baseURL, client.allowInsecureHTTP); err != nil {
		return nil, err
	}
	// Never forward X-API-Key to a redirect target, including custom clients.
	httpClient := *client.httpClient
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	client.httpClient = &httpClient
	return client, nil
}

func assertSecureBaseURL(baseURL string, allowInsecureHTTP bool) error {
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("zpljet: invalid base URL %q: %w", baseURL, err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if allowInsecureHTTP || host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return fmt.Errorf(
			"zpljet: refusing to send your API key over plaintext http:// to %s — use https, or pass WithAllowInsecureHTTP() for local/testing",
			u.Host)
	default:
		return fmt.Errorf("zpljet: unsupported base URL scheme %q", u.Scheme)
	}
}

// Convert renders ZPL and returns the raw file bytes (PDF or PNG). Nothing
// is stored server-side — available on every plan.
//
// Errors are returned as *Error (API errors — branch on Error.Code),
// *ConnError (network failures and timeouts, after retries), or the
// context's error if ctx is cancelled.
func (c *Client) Convert(ctx context.Context, req ConvertRequest) (*LabelData, error) {
	resp, body, err := c.doWithRetries(ctx, req, "")
	if err != nil {
		return nil, err
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return &LabelData{
		Data:        body,
		ContentType: contentType,
		ID:          resp.Header.Get("X-Conversion-Id"),
	}, nil
}

// ConvertToURL renders and hosts a label, then returns its public URL.
func (c *Client) ConvertToURL(ctx context.Context, req ConvertRequest) (*HostedLabel, error) {
	_, body, err := c.doWithRetries(ctx, req, "url")
	if err != nil {
		return nil, err
	}
	var hosted HostedLabel
	if err := json.Unmarshal(body, &hosted); err != nil {
		return nil, fmt.Errorf("zpljet: invalid JSON in API response: %w", err)
	}
	if hosted.ID == "" || hosted.URL == "" || hosted.ExpiresAt.IsZero() ||
		hosted.Pages < 1 || hosted.RetentionDays < 1 {
		return nil, errors.New("zpljet: invalid hosted conversion payload in API response")
	}
	return &hosted, nil
}

func (c *Client) doWithRetries(
	ctx context.Context, req ConvertRequest, output string,
) (*http.Response, []byte, error) {
	payload, err := marshalRequest(req, output)
	if err != nil {
		return nil, nil, err
	}
	return c.doWithRetriesPayload(ctx, "/v1/convert", payload)
}

func (c *Client) doWithRetriesPayload(
	ctx context.Context, path string, payload []byte,
) (*http.Response, []byte, error) {
	for attempt := 0; ; attempt++ {
		resp, body, attemptErr := c.doOnce(ctx, path, payload)
		if attemptErr == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, body, nil
		}

		var failure error
		var headerRetryAfter time.Duration
		var headerRetryAfterOK bool
		if attemptErr != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			failure = attemptErr
		} else {
			failure = parseError(resp.StatusCode, body)
			headerRetryAfter, headerRetryAfterOK =
				parseRetryAfterHeader(resp.Header.Get("Retry-After"))
		}

		if attempt >= c.maxRetries || !isRetryable(failure) {
			return nil, nil, failure
		}
		delay := retryDelay(failure, attempt, headerRetryAfter, headerRetryAfterOK)
		if err := sleepCtx(ctx, delay); err != nil {
			return nil, nil, err
		}
	}
}

func (c *Client) doOnce(
	ctx context.Context, path string, payload []byte,
) (*http.Response, []byte, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	url := c.baseURL + path
	httpReq, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, &ConnError{msg: "building request: " + err.Error(), cause: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-API-Key", c.apiKey)
	httpReq.Header.Set("User-Agent", "zpljet-go/"+Version)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if attemptCtx.Err() != nil && ctx.Err() == nil {
			return nil, nil, &ConnError{
				msg:     fmt.Sprintf("request to %s timed out after %s", url, c.timeout),
				cause:   err,
				timeout: true,
			}
		}
		return nil, nil, &ConnError{msg: fmt.Sprintf("request to %s failed: %v", url, err), cause: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, &ConnError{msg: "reading response body: " + err.Error(), cause: err}
	}
	return resp, body, nil
}

func marshalRequest(req ConvertRequest, output string) ([]byte, error) {
	payload, err := json.Marshal(struct {
		ConvertRequest
		Output string `json:"output,omitempty"`
	}{req, output})
	if err != nil {
		return nil, fmt.Errorf("zpljet: encoding request: %w", err)
	}
	return payload, nil
}

func isRetryable(err error) bool {
	var connErr *ConnError
	if errors.As(err, &connErr) {
		return true
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Status == http.StatusTooManyRequests {
		return true
	}
	return apiErr.Status >= 500 && apiErr.Code != CodeConversionFailed
}

func parseRetryAfterHeader(value string) (delay time.Duration, ok bool) {
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 {
		return retryAfterDuration(seconds), true
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := time.Until(when); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}

func retryDelay(
	err error, attempt int, headerRetryAfter time.Duration, headerRetryAfterOK bool,
) time.Duration {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		if _, ok := apiErr.Raw["retryAfter"].(float64); ok {
			if apiErr.RetryAfter > maxRetryDelay {
				return maxRetryDelay
			}
			return apiErr.RetryAfter
		}
	}
	if headerRetryAfterOK {
		if headerRetryAfter > maxRetryDelay {
			return maxRetryDelay
		}
		return headerRetryAfter
	}
	backoff := baseRetryDelay << attempt
	jitter := time.Duration(rand.Int63n(int64(backoff)/4 + 1))
	if backoff+jitter > maxRetryDelay {
		return maxRetryDelay
	}
	return backoff + jitter
}

func sleepCtx(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
