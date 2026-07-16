package zpljet

import (
	"encoding/json"
	"fmt"
	"time"
)

// Stable machine-readable error codes returned by the public API — compare
// them against Error.Code. Full reference: https://zpljet.com/docs/errors
const (
	// CodeInvalidRequest — 400: the request body failed validation;
	// Error.Param names the offending field.
	CodeInvalidRequest = "invalid_request"
	// CodeMissingAPIKey — 401: no X-API-Key header was sent.
	CodeMissingAPIKey = "missing_api_key"
	// CodeInvalidAPIKey — 401: the key is invalid, disabled, or expired.
	CodeInvalidAPIKey = "invalid_api_key"
	// CodePayloadTooLarge — 413: request body exceeded the API limit.
	CodePayloadTooLarge = "payload_too_large"
	// CodeQuotaExceeded — 402: the monthly conversion quota is used up;
	// see Error.Quota, Error.Used, and Error.ResetsAt.
	CodeQuotaExceeded = "quota_exceeded"
	// CodeHostingNotAllowed — 403: hosted URLs are not available on this plan.
	CodeHostingNotAllowed = "hosting_not_allowed"
	// CodeNoRetentionEnforced — 403: enforced no-retention mode forbids hosting.
	CodeNoRetentionEnforced = "no_retention_enforced"
	// CodeRateLimitExceeded — 429: too many requests for this API key; the
	// SDK retries these automatically (honoring Error.RetryAfter) before
	// returning the error.
	CodeRateLimitExceeded = "rate_limit_exceeded"
	// CodeConversionFailed — 502: the rendering engine could not process the
	// ZPL; not retried automatically. See Error.ConversionID.
	CodeConversionFailed = "conversion_failed"
	// CodeServiceUnavailable — 503: render engine temporarily unavailable;
	// the request was not charged against quota. Retry after RetryAfter.
	CodeServiceUnavailable = "service_unavailable"
)

// Error is an HTTP error response from the API. Every error body has the
// shape {"error": {"code", "message", …context, "docUrl"}}; branch on the
// stable Code with errors.As:
//
//	var apiErr *zpljet.Error
//	if errors.As(err, &apiErr) && apiErr.Code == zpljet.CodeRateLimitExceeded {
//	    time.Sleep(apiErr.RetryAfter)
//	}
type Error struct {
	// Status is the HTTP status code.
	Status int
	// Code is the stable machine-readable code — safe to branch on.
	Code string
	// Message is human-readable and may change; don't parse it.
	Message string
	// DocURL links to the docs entry for this code.
	DocURL string

	// Param names the invalid field (CodeInvalidRequest), e.g. "zpl".
	Param string
	// Plan is the account's plan id (CodeQuotaExceeded), e.g. "free".
	Plan string
	// Quota is the plan's monthly quota (CodeQuotaExceeded).
	Quota int
	// Used is the number of conversions used this month (CodeQuotaExceeded).
	Used int
	// ResetsAt is when the quota resets, ISO 8601 UTC (CodeQuotaExceeded).
	ResetsAt string
	// RetryAfter is how long to wait before retrying (CodeRateLimitExceeded).
	RetryAfter time.Duration
	// RetryAt is when to retry, ISO 8601 UTC (CodeRateLimitExceeded).
	RetryAt string
	// ConversionID identifies the failed attempt (CodeConversionFailed) —
	// quote it when contacting support.
	ConversionID string

	// Raw is the full parsed error object, including any context fields.
	Raw map[string]any
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("zpljet: %s (HTTP %d, code %q)", e.Message, e.Status, e.Code)
	}
	return fmt.Sprintf("zpljet: %s (HTTP %d)", e.Message, e.Status)
}

// ConnError means the request never produced a usable API response — DNS
// failure, connection reset, TLS error, or a per-attempt timeout. The SDK
// retries these automatically before returning one.
type ConnError struct {
	msg     string
	cause   error
	timeout bool
}

// Error implements the error interface.
func (e *ConnError) Error() string { return "zpljet: " + e.msg }

// Unwrap exposes the underlying transport error for errors.Is/As.
func (e *ConnError) Unwrap() error { return e.cause }

// Timeout reports whether the failure was a per-attempt timeout.
func (e *ConnError) Timeout() bool { return e.timeout }

// parseError builds an *Error from a non-2xx response body, tolerating
// non-JSON bodies (e.g. gateway error pages).
func parseError(status int, body []byte) *Error {
	apiErr := &Error{
		Status:  status,
		Message: fmt.Sprintf("HTTP %d error from the ZPLJet API", status),
	}
	var envelope struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Error == nil {
		return apiErr
	}

	raw := envelope.Error
	apiErr.Raw = raw
	apiErr.Code = stringField(raw, "code")
	if message := stringField(raw, "message"); message != "" {
		apiErr.Message = message
	}
	apiErr.DocURL = stringField(raw, "docUrl")
	apiErr.Param = stringField(raw, "param")
	apiErr.Plan = stringField(raw, "plan")
	apiErr.Quota = intField(raw, "quota")
	apiErr.Used = intField(raw, "used")
	apiErr.ResetsAt = stringField(raw, "resetsAt")
	apiErr.RetryAt = stringField(raw, "retryAt")
	apiErr.ConversionID = stringField(raw, "conversionId")
	if seconds, ok := raw["retryAfter"].(float64); ok {
		apiErr.RetryAfter = time.Duration(seconds * float64(time.Second))
	}
	return apiErr
}

func stringField(raw map[string]any, key string) string {
	value, _ := raw[key].(string)
	return value
}

func intField(raw map[string]any, key string) int {
	value, ok := raw[key].(float64)
	if !ok {
		return 0
	}
	return int(value)
}
