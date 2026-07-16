package zpljet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testZPL = "^XA^FO50,50^A0N,50,50^FDHello^FS^XZ"

// newTestClient points a client (with near-instant backoff) at a test server.
func newTestClient(t *testing.T, handler http.Handler, opts ...Option) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New("zpl_test", append([]Option{WithBaseURL(server.URL), WithMaxRetries(0)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	client.baseRetryDelay = time.Millisecond
	return client
}

func writeError(w http.ResponseWriter, status int, code, message, extraJSON string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	extra := ""
	if extraJSON != "" {
		extra = extraJSON + ","
	}
	fmt.Fprintf(w,
		`{"error":{"code":%q,"message":%q,%s"docUrl":"https://zpljet.com/docs/errors#%s"}}`,
		code, message, extra, code)
}

func writePDF(w http.ResponseWriter, id string) {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("X-Conversion-Id", id)
	_, _ = w.Write([]byte("%PDF-fake"))
}

func TestNewRequiresAPIKey(t *testing.T) {
	if _, err := New("   "); err == nil {
		t.Fatal("expected an error for a blank API key")
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	client, err := New("zpl_test")
	if err != nil {
		t.Fatal(err)
	}
	if client.baseURL != "https://api.zpljet.com" {
		t.Errorf("baseURL = %q", client.baseURL)
	}
	if client.timeout != 60*time.Second {
		t.Errorf("timeout = %v", client.timeout)
	}
	if client.maxRetries != 2 {
		t.Errorf("maxRetries = %d", client.maxRetries)
	}
}

func TestNewValidatesAndCapsRetries(t *testing.T) {
	if _, err := New("zpl_test", WithMaxRetries(-1)); err == nil {
		t.Fatal("expected an error for negative max retries")
	}
	client, err := New("zpl_test", WithMaxRetries(99))
	if err != nil {
		t.Fatal(err)
	}
	if client.maxRetries != maxRetriesCap {
		t.Errorf("maxRetries = %d, want %d", client.maxRetries, maxRetriesCap)
	}
}

func TestWithBaseURLStripsTrailingSlashes(t *testing.T) {
	client, _ := New("zpl_test", WithBaseURL("http://localhost:3000//"))
	if client.baseURL != "http://localhost:3000" {
		t.Errorf("baseURL = %q", client.baseURL)
	}
}

func TestNewRejectsInsecureRemoteBaseURL(t *testing.T) {
	if _, err := New("zpl_test", WithBaseURL("http://api.example.com")); err == nil {
		t.Fatal("expected insecure remote base URL to be rejected")
	}
	if _, err := New(
		"zpl_test",
		WithBaseURL("http://api.example.com"),
		WithAllowInsecureHTTP(),
	); err != nil {
		t.Fatalf("explicit insecure opt-in failed: %v", err)
	}
}

func TestNewRejectsNilHTTPClient(t *testing.T) {
	if _, err := New("zpl_test", WithHTTPClient(nil)); err == nil {
		t.Fatal("expected nil HTTP client to be rejected")
	}
}

func TestCustomHTTPClientCannotFollowRedirects(t *testing.T) {
	custom := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return nil },
	}
	client, err := New("zpl_test", WithHTTPClient(custom))
	if err != nil {
		t.Fatal(err)
	}

	err = client.httpClient.CheckRedirect(&http.Request{}, nil)
	if !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect error = %v", err)
	}
	if custom.CheckRedirect == nil {
		t.Fatal("custom client was mutated")
	}
}

func TestConvertPostsJSONWithAPIKeyAndUserAgent(t *testing.T) {
	var captured *http.Request
	var capturedBody map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		writePDF(w, "conv_123")
	}))

	_, err := client.Convert(context.Background(), ConvertRequest{ZPL: testZPL, DPMM: 12, Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	if captured.URL.Path != "/v1/convert" || captured.Method != http.MethodPost {
		t.Errorf("%s %s", captured.Method, captured.URL.Path)
	}
	if got := captured.Header.Get("X-API-Key"); got != "zpl_test" {
		t.Errorf("X-API-Key = %q", got)
	}
	if got := captured.Header.Get("User-Agent"); got != "zpljet-go/"+Version {
		t.Errorf("User-Agent = %q", got)
	}
	want := map[string]any{"zpl": testZPL, "dpmm": float64(12), "format": "pdf"}
	if len(capturedBody) != len(want) {
		t.Errorf("body = %v, want %v", capturedBody, want)
	}
	for key, value := range want {
		if capturedBody[key] != value {
			t.Errorf("body[%q] = %v, want %v", key, capturedBody[key], value)
		}
	}
}

func TestConvertOmitsUnsetParameters(t *testing.T) {
	var capturedBody map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		writePDF(w, "conv_123")
	}))

	if _, err := client.Convert(context.Background(), ConvertRequest{ZPL: testZPL}); err != nil {
		t.Fatal(err)
	}
	if len(capturedBody) != 1 {
		t.Errorf(`body = %v, want just {"zpl": …}`, capturedBody)
	}
}

func TestConvertReturnsBytesContentTypeAndID(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writePDF(w, "conv_abc")
	}))

	label, err := client.Convert(context.Background(), ConvertRequest{ZPL: testZPL})
	if err != nil {
		t.Fatal(err)
	}
	if string(label.Data) != "%PDF-fake" {
		t.Errorf("Data = %q", label.Data)
	}
	if label.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q", label.ContentType)
	}
	if label.ID != "conv_abc" {
		t.Errorf("ID = %q", label.ID)
	}
}

func TestConvertToURLReturnsParsedHostedLabel(t *testing.T) {
	var capturedBody map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"conv_456","url":"https://files.example/conv_456.pdf",
			"pages":2,"retentionDays":7,
			"expiresAt":"2026-07-10T00:00:00.000Z"}`)
	}))

	hosted, err := client.ConvertToURL(context.Background(), ConvertRequest{ZPL: testZPL})
	if err != nil {
		t.Fatal(err)
	}
	if capturedBody["output"] != "url" {
		t.Errorf("output = %v, want url", capturedBody["output"])
	}
	if hosted.Pages != 2 || hosted.RetentionDays != 7 || hosted.ID != "conv_456" {
		t.Errorf("hosted = %+v", hosted)
	}
	if hosted.ExpiresAt.IsZero() || !hosted.ExpiresAt.After(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ExpiresAt = %v", hosted.ExpiresAt)
	}
}

func TestConvertToURLRejectsMalformedSuccessWithoutRetry(t *testing.T) {
	var calls int
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"conv_456"}`)
	}), WithMaxRetries(5))

	_, err := client.ConvertToURL(context.Background(), ConvertRequest{ZPL: testZPL})
	if err == nil || !strings.Contains(err.Error(), "invalid hosted conversion payload") {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestErrorMapping(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 402, CodeQuotaExceeded, "Monthly quota exceeded",
			`"plan":"free","quota":500,"used":500,"resetsAt":"2026-08-01T00:00:00.000Z"`)
	}))

	_, err := client.Convert(context.Background(), ConvertRequest{ZPL: testZPL})
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if apiErr.Status != 402 || apiErr.Code != CodeQuotaExceeded {
		t.Errorf("Status=%d Code=%q", apiErr.Status, apiErr.Code)
	}
	if apiErr.Plan != "free" || apiErr.Quota != 500 || apiErr.Used != 500 {
		t.Errorf("quota context = %+v", apiErr)
	}
	if apiErr.ResetsAt != "2026-08-01T00:00:00.000Z" {
		t.Errorf("ResetsAt = %q", apiErr.ResetsAt)
	}
	if apiErr.DocURL != "https://zpljet.com/docs/errors#quota_exceeded" {
		t.Errorf("DocURL = %q", apiErr.DocURL)
	}
}

func TestInvalidRequestCarriesParam(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 400, CodeInvalidRequest, "zpl: no label found", `"param":"zpl"`)
	}))

	_, err := client.Convert(context.Background(), ConvertRequest{ZPL: "nope"})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Param != "zpl" {
		t.Fatalf("err = %v", err)
	}
}

func TestNonJSONErrorBodyGetsDefaultMessage(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("<html>Bad Gateway</html>"))
	}))

	_, err := client.Convert(context.Background(), ConvertRequest{ZPL: testZPL})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 503 {
		t.Fatalf("err = %v", err)
	}
	if apiErr.Message != "HTTP 503 error from the ZPLJet API" {
		t.Errorf("Message = %q", apiErr.Message)
	}
}

func TestRetriesA429AndSucceeds(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			writeError(w, 429, CodeRateLimitExceeded, "slow down", `"retryAfter":0`)
			return
		}
		writePDF(w, "conv_123")
	}), WithMaxRetries(2))

	label, err := client.Convert(context.Background(), ConvertRequest{ZPL: testZPL})
	if err != nil {
		t.Fatal(err)
	}
	if label.ContentType != "application/pdf" || calls.Load() != 2 {
		t.Errorf("ContentType=%q calls=%d", label.ContentType, calls.Load())
	}
}

func TestRateLimitContextOnceExhausted(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeError(w, 429, CodeRateLimitExceeded, "slow down",
			`"retryAfter":0.001,"retryAt":"2026-07-07T00:00:01.000Z"`)
	}), WithMaxRetries(2))

	_, err := client.Convert(context.Background(), ConvertRequest{ZPL: testZPL})
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v", err)
	}
	if apiErr.RetryAfter != time.Millisecond || apiErr.RetryAt == "" {
		t.Errorf("RetryAfter=%v RetryAt=%q", apiErr.RetryAfter, apiErr.RetryAt)
	}
	if calls.Load() != 3 { // 1 attempt + 2 retries
		t.Errorf("calls = %d", calls.Load())
	}
}

func TestHonorsRetryAfterHeaderWhenBodyIsNotJSON(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("<html>Too Many Requests</html>"))
			return
		}
		writePDF(w, "conv_123")
	}), WithMaxRetries(1))
	// Make backoff long so the test only passes if the header (0s) is used.
	client.baseRetryDelay = 10 * time.Second

	start := time.Now()
	label, err := client.Convert(context.Background(), ConvertRequest{ZPL: testZPL})
	if err != nil {
		t.Fatal(err)
	}
	if label.ContentType != "application/pdf" || calls.Load() != 2 {
		t.Errorf("ContentType=%q calls=%d", label.ContentType, calls.Load())
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %v — Retry-After header was not honored", elapsed)
	}
}

func TestRetryAfterZeroRetriesImmediately(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			writeError(w, 429, CodeRateLimitExceeded, "slow down", `"retryAfter":0`)
			return
		}
		writePDF(w, "conv_123")
	}), WithMaxRetries(1))
	// A retryAfter of 0 must mean "immediately" — not fall through to backoff.
	client.baseRetryDelay = 10 * time.Second

	start := time.Now()
	if _, err := client.Convert(context.Background(), ConvertRequest{ZPL: testZPL}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %v — explicit retryAfter:0 fell through to backoff", elapsed)
	}
}

func TestParseRetryAfterHeader(t *testing.T) {
	if d, ok := parseRetryAfterHeader("7"); !ok || d != 7*time.Second {
		t.Errorf("delta-seconds = %v ok=%v", d, ok)
	}
	if d, ok := parseRetryAfterHeader("0"); !ok || d != 0 {
		t.Errorf("explicit zero = %v ok=%v", d, ok)
	}
	if _, ok := parseRetryAfterHeader(""); ok {
		t.Error("empty should not be ok")
	}
	if _, ok := parseRetryAfterHeader("soon"); ok {
		t.Error("garbage should not be ok")
	}
	if d, ok := parseRetryAfterHeader("Tue, 07 Jul 2099 00:00:00 GMT"); !ok || d <= 0 {
		t.Errorf("http-date = %v ok=%v", d, ok)
	}
}

func TestConvertToURLParseFailureIsNotAConnError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json at all"))
	}))

	_, err := client.ConvertToURL(context.Background(), ConvertRequest{ZPL: testZPL})
	if err == nil {
		t.Fatal("expected an error")
	}
	var connErr *ConnError
	if errors.As(err, &connErr) {
		t.Fatalf("parse failure must not be a *ConnError (looks retryable): %v", err)
	}
	var apiErr *Error
	if errors.As(err, &apiErr) {
		t.Fatalf("parse failure must not be an *Error either: %v", err)
	}
}

func TestConversionFailedIsNeverRetried(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeError(w, 502, CodeConversionFailed, "Could not render", `"conversionId":"conv_x"`)
	}), WithMaxRetries(5))

	_, err := client.Convert(context.Background(), ConvertRequest{ZPL: testZPL})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.ConversionID != "conv_x" {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d", calls.Load())
	}
}

func TestNeverRetries4xxClientErrors(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeError(w, 400, CodeInvalidRequest, "bad", `"param":"zpl"`)
	}), WithMaxRetries(5))

	_, _ = client.Convert(context.Background(), ConvertRequest{ZPL: "x"})
	if calls.Load() != 1 {
		t.Errorf("calls = %d", calls.Load())
	}
}

func TestRetriesTransient5xx(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("oops"))
			return
		}
		writePDF(w, "conv_123")
	}), WithMaxRetries(1))

	label, err := client.Convert(context.Background(), ConvertRequest{ZPL: testZPL})
	if err != nil {
		t.Fatal(err)
	}
	if label.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q", label.ContentType)
	}
}

func TestRetriesConnectionErrors(t *testing.T) {
	// A server that immediately closes the connection on the first call.
	var calls atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("no hijacker")
			}
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		writePDF(w, "conv_123")
	}), WithMaxRetries(1))

	label, err := client.Convert(context.Background(), ConvertRequest{ZPL: testZPL})
	if err != nil {
		t.Fatal(err)
	}
	if label.ID != "conv_123" || calls.Load() != 2 {
		t.Errorf("ID=%q calls=%d", label.ID, calls.Load())
	}
}

func TestPersistentConnectionFailureReturnsConnError(t *testing.T) {
	client, err := New("zpl_test",
		WithBaseURL("http://127.0.0.1:1"), // nothing listens here
		WithMaxRetries(1))
	if err != nil {
		t.Fatal(err)
	}
	client.baseRetryDelay = time.Millisecond

	_, err = client.Convert(context.Background(), ConvertRequest{ZPL: testZPL})
	var connErr *ConnError
	if !errors.As(err, &connErr) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if connErr.Timeout() {
		t.Error("refused connection should not be a timeout")
	}
}

// hangingHandler blocks until the client goes away or unblock is called. It
// drains the body first — the server only notices a dropped client (and
// cancels r.Context()) once the request body has been consumed — and also
// waits on an unblock channel as a safety net so httptest's server.Close
// (which waits for outstanding handlers) can never deadlock. Register
// t.Cleanup(unblock) AFTER newTestClient so it runs before server.Close.
func hangingHandler(calls *atomic.Int32) (handler http.Handler, unblock func()) {
	done := make(chan struct{})
	var once sync.Once
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-done:
		}
	})
	return handler, func() { once.Do(func() { close(done) }) }
}

func TestTimesOutAnAttempt(t *testing.T) {
	handler, unblock := hangingHandler(nil)
	client := newTestClient(t, handler, WithTimeout(50*time.Millisecond))
	t.Cleanup(unblock)

	_, err := client.Convert(context.Background(), ConvertRequest{ZPL: testZPL})
	var connErr *ConnError
	if !errors.As(err, &connErr) || !connErr.Timeout() {
		t.Fatalf("err = %v (%T)", err, err)
	}
}

func TestCallerCancellationPropagatesAndIsNeverRetried(t *testing.T) {
	var calls atomic.Int32
	handler, unblock := hangingHandler(&calls)
	client := newTestClient(t, handler, WithMaxRetries(5))
	t.Cleanup(unblock)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := client.Convert(ctx, ConvertRequest{ZPL: testZPL})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d", calls.Load())
	}
}

func TestAlreadyCancelledContextShortCircuits(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writePDF(w, "conv_123")
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Convert(ctx, ConvertRequest{ZPL: testZPL})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 0 {
		t.Errorf("calls = %d", calls.Load())
	}
}
