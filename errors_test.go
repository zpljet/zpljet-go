package zpljet

import (
	"strings"
	"testing"
	"time"
)

func TestParseErrorReadsAllContextFields(t *testing.T) {
	body := []byte(`{"error":{
		"code":"rate_limit_exceeded","message":"slow down",
		"retryAfter":2,"retryAt":"2026-07-07T00:00:02.000Z",
		"docUrl":"https://zpljet.com/docs/errors#rate_limit_exceeded"}}`)
	apiErr := parseError(429, body)

	if apiErr.Code != CodeRateLimitExceeded || apiErr.Message != "slow down" {
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
	apiErr := parseError(413, []byte(`{"error":{"code":"payload_too_large","message":"too large"}}`))
	if apiErr.Code != CodePayloadTooLarge || apiErr.Status != 413 {
		t.Errorf("parsed = %+v", apiErr)
	}
}

func TestParseErrorToleratesGarbage(t *testing.T) {
	for _, body := range [][]byte{
		[]byte("<html>Bad Gateway</html>"),
		[]byte(""),
		[]byte(`{"unexpected":"shape"}`),
		[]byte(`{"error":"a string, not an object"}`),
	} {
		apiErr := parseError(503, body)
		if apiErr.Status != 503 || !strings.Contains(apiErr.Message, "HTTP 503") {
			t.Errorf("parseError(%q) = %+v", body, apiErr)
		}
	}
}

func TestParseErrorIgnoresWrongTypes(t *testing.T) {
	apiErr := parseError(429, []byte(`{"error":{"code":"rate_limit_exceeded","retryAfter":"soon","quota":"lots"}}`))
	if apiErr.RetryAfter != 0 || apiErr.Quota != 0 {
		t.Errorf("parsed = %+v", apiErr)
	}
}

func TestErrorStringIncludesStatusAndCode(t *testing.T) {
	apiErr := parseError(402, []byte(`{"error":{"code":"quota_exceeded","message":"Monthly quota exceeded"}}`))
	text := apiErr.Error()
	for _, want := range []string{"quota_exceeded", "402", "Monthly quota exceeded"} {
		if !strings.Contains(text, want) {
			t.Errorf("Error() = %q, missing %q", text, want)
		}
	}
}
