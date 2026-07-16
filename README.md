# zpljet-go

Official Go SDK for the [ZPLJet](https://zpljet.com) API — fast ZPL → PDF/PNG conversion.

[![Go Reference](https://pkg.go.dev/badge/github.com/zpljet/zpljet-go.svg)](https://pkg.go.dev/github.com/zpljet/zpljet-go)
[![CI](https://github.com/zpljet/zpljet-go/actions/workflows/ci.yml/badge.svg)](https://github.com/zpljet/zpljet-go/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)

- **Zero dependencies** — stdlib `net/http` only
- **Reliable by default** — automatic retries with exponential backoff (honoring `Retry-After`), per-attempt timeouts, full `context.Context` support
- **Typed errors** — branch on stable error codes with `errors.As`, never on strings
- Go ≥ 1.21

## Installation

```sh
go get github.com/zpljet/zpljet-go
```

## Quickstart

Create an API key in the [dashboard](https://zpljet.com/dashboard) (keys look like `zpl_…`), then:

```go
package main

import (
    "context"
    "log"
    "os"

    zpljet "github.com/zpljet/zpljet-go"
)

func main() {
    client, err := zpljet.New(os.Getenv("ZPLJET_API_KEY"))
    if err != nil {
        log.Fatal(err)
    }

    label, err := client.Convert(context.Background(), zpljet.ConvertRequest{
        ZPL: "^XA^FO50,50^A0N,50,50^FDHello^FS^XZ",
    })
    if err != nil {
        log.Fatal(err)
    }

    // label.Data is the raw PDF bytes — nothing is stored server-side.
    os.WriteFile("label.pdf", label.Data, 0o644)
}
```

> **Keep your API key server-side.** Anyone with the key can spend your
> quota. Create one `Client` and reuse it — it is safe for concurrent use.

## Usage

### Convert to PDF or PNG

`ConvertRequest` covers the data-mode conversion parameters; zero values use
the API defaults. Hosted URL output is exposed separately as `ConvertToURL`,
not as an `output` field:

```go
label, err := client.Convert(ctx, zpljet.ConvertRequest{
    ZPL:      zpl,
    Format:   zpljet.FormatPNG, // FormatPDF (default) | FormatPNG
    DPMM:     12,               // 6 | 8 (default, 203 dpi) | 12 (300 dpi) | 24 (600 dpi)
    WidthMm:  101.6,            // label width, default 4 in
    HeightMm: 152.4,            // label height, default 6 in
})

label.Data        // []byte — the file
label.ContentType // "application/pdf" | "image/png"
label.ID          // conversion id (shows up in your dashboard)
```

### Hosted URLs (paid plans)

`ConvertToURL` has ZPLJet host the file and returns a public link instead of
the bytes. Files are retained for your account's retention window (a
dashboard setting, up to your plan's maximum).

```go
hosted, err := client.ConvertToURL(ctx, zpljet.ConvertRequest{ZPL: zpl})

hosted.URL           // public URL to the PDF (works until the file is deleted)
hosted.Pages         // pages rendered (one per ^XA…^XZ block)
hosted.RetentionDays // how long the file is kept
hosted.ExpiresAt     // time.Time — when the file is deleted and the URL stops working (UTC)
```

### Error handling

API errors are `*zpljet.Error` with a stable `Code` and typed context
fields; transport failures are `*zpljet.ConnError`. Branch with `errors.As`:

```go
label, err := client.Convert(ctx, zpljet.ConvertRequest{ZPL: zpl})
if err != nil {
    var apiErr *zpljet.Error
    var connErr *zpljet.ConnError
    switch {
    case errors.As(err, &apiErr):
        switch apiErr.Code {
        case zpljet.CodeInvalidRequest:
            log.Printf("invalid %s: %s", apiErr.Param, apiErr.Message)
        case zpljet.CodeQuotaExceeded:
            log.Printf("quota %d/%d used, resets %s", apiErr.Used, apiErr.Quota, apiErr.ResetsAt)
        case zpljet.CodeRateLimitExceeded:
            log.Printf("rate limited, retry after %s", apiErr.RetryAfter) // already auto-retried
        case zpljet.CodeConversionFailed:
            log.Printf("engine rejected the ZPL (conversion %s)", apiErr.ConversionID)
        }
    case errors.As(err, &connErr):
        log.Printf("network problem (timeout=%v): %v", connErr.Timeout(), connErr) // already auto-retried
    default:
        log.Print(err) // context cancellation, …
    }
}
```

| `Error.Code` | Status | Context fields |
| --- | --- | --- |
| `CodeInvalidRequest` | 400 | `Param` |
| `CodeMissingAPIKey` · `CodeInvalidAPIKey` | 401 | — |
| `CodeQuotaExceeded` | 402 | `Plan`, `Quota`, `Used`, `ResetsAt` |
| `CodeHostingNotAllowed` · `CodeNoRetentionEnforced` | 403 | — |
| `CodePayloadTooLarge` | 413 | — |
| `CodeRateLimitExceeded` | 429 | `RetryAfter`, `RetryAt` |
| `CodeConversionFailed` | 502 | `ConversionID` |
| `CodeServiceUnavailable` | 503 | `RetryAfter` |

Every `*Error` also carries `Status`, `Message`, `DocURL`, and the raw
payload in `Raw`. Full code reference:
[zpljet.com/docs/errors](https://zpljet.com/docs/errors).

### Retries

Rate limits (429), transient server errors (5xx), timeouts, and network
failures are retried automatically — up to 2 times by default, with
exponential backoff, honoring the server's `Retry-After`. A 503
`service_unavailable` means the render engine is temporarily unavailable; the
request was not charged against quota. A 502
`conversion_failed` is **not** retried: it means the engine rejected the ZPL
itself, so a retry would fail identically.

### Configuration

```go
client, err := zpljet.New("zpl_…",
    zpljet.WithBaseURL("https://api.zpljet.com"), // default
    zpljet.WithTimeout(10*time.Second),           // per-attempt timeout (default 60s)
    zpljet.WithMaxRetries(5),                     // automatic retries (default 2)
    zpljet.WithHTTPClient(customHTTPClient),      // custom transport (proxies, tests)
)
```

Cancellation and deadlines use standard `context.Context`; a caller-cancelled
context is returned as-is (never retried), a per-attempt timeout surfaces as
a `*ConnError` with `Timeout() == true` after retries.

## Examples

Runnable examples live in [`examples/`](./examples):

```sh
ZPLJET_API_KEY=zpl_… go run ./examples/convert-to-pdf
# also: convert-to-png | hosted-url | error-handling | batch-with-concurrency
```

## Contributing & development

```sh
go vet ./...
go test ./...       # unit tests (local httptest servers, no real network)

# End-to-end tests against the live API (uses your quota):
ZPLJET_API_KEY=zpl_… go test -run E2E -v
```

## License

[MIT](./LICENSE)
