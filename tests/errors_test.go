package tests

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zpljet/zpljet-go"
)

func errorFromResponse(t *testing.T, status int, body string) *zpljet.Error {
	t.Helper()
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	_, err := client.Convert(context.Background(), zpljet.ConvertRequest{ZPL: testZPL})
	var apiErr *zpljet.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v", err)
	}
	return apiErr
}

func TestParseErrorReadsAllContextFields(t *testing.T) {
	body := `{"error":{
		"code":"rate_limit_exceeded","message":"slow down",
		"retryAfter":2,"retryAt":"2026-07-07T00:00:02.000Z",
		"docUrl":"https://zpljet.com/docs/errors#rate_limit_exceeded"}}`
	apiErr := errorFromResponse(t, 429, body)

	if apiErr.Code != zpljet.CodeRateLimitExceeded || apiErr.Message != "slow down" {
		t.Errorf("parsed = %+v", apiErr)
	}
	if apiErr.RetryAfter != 2*time.Second {
		t.Errorf("RetryAfter = %v", apiErr.RetryAfter)
	}
	if apiErr.Raw["retryAt"] != "2026-07-07T00:00:02.000Z" {
		t.Errorf("Raw = %v", apiErr.Raw)
	}
}

func TestPayloadTooLargeCode(t *testing.T) {
	apiErr := errorFromResponse(
		t,
		413,
		`{"error":{"code":"payload_too_large","message":"too large"}}`,
	)
	if apiErr.Code != zpljet.CodePayloadTooLarge || apiErr.Status != 413 {
		t.Errorf("parsed = %+v", apiErr)
	}
}

func TestParseErrorToleratesGarbage(t *testing.T) {
	for _, body := range []string{
		"<html>Bad Gateway</html>",
		"",
		`{"unexpected":"shape"}`,
		`{"error":"a string, not an object"}`,
	} {
		apiErr := errorFromResponse(t, 503, body)
		if apiErr.Status != 503 || !strings.Contains(apiErr.Message, "HTTP 503") {
			t.Errorf("body %q: error = %+v", body, apiErr)
		}
	}
}

func TestParseErrorIgnoresWrongTypes(t *testing.T) {
	apiErr := errorFromResponse(
		t,
		429,
		`{"error":{"code":"rate_limit_exceeded","retryAfter":"soon","quota":"lots"}}`,
	)
	if apiErr.RetryAfter != 0 || apiErr.Quota != 0 {
		t.Errorf("parsed = %+v", apiErr)
	}
}

func TestParseErrorClampsRetryAfter(t *testing.T) {
	for body, want := range map[string]time.Duration{
		`{"error":{"retryAfter":-1}}`:    0,
		`{"error":{"retryAfter":1e100}}`: 30 * time.Second,
	} {
		if got := errorFromResponse(t, 429, body).RetryAfter; got != want {
			t.Errorf("body %s: RetryAfter = %v, want %v", body, got, want)
		}
	}
}

func TestErrorStringIncludesStatusAndCode(t *testing.T) {
	apiErr := errorFromResponse(
		t,
		402,
		`{"error":{"code":"quota_exceeded","message":"Monthly quota exceeded"}}`,
	)
	text := apiErr.Error()
	for _, want := range []string{"quota_exceeded", "402", "Monthly quota exceeded"} {
		if !strings.Contains(text, want) {
			t.Errorf("Error() = %q, missing %q", text, want)
		}
	}
}
