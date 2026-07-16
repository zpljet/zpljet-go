# zpljet-go

Official Go SDK for the [ZPLJet](https://zpljet.com) API — fast ZPL → PDF/PNG conversion.

[![Go Reference](https://pkg.go.dev/badge/github.com/zpljet/zpljet-go.svg)](https://pkg.go.dev/github.com/zpljet/zpljet-go)
[![CI](https://github.com/zpljet/zpljet-go/actions/workflows/ci.yml/badge.svg)](https://github.com/zpljet/zpljet-go/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/zpljet/zpljet-go/blob/main/LICENSE)

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

    if err := os.WriteFile("label.pdf", label.Data, 0o644); err != nil {
        log.Fatal(err)
    }
}
```

> **Keep your API key server-side.** Anyone with the key can spend your
> quota. Create one `Client` and reuse it — it is safe for concurrent use.

## Usage

### Convert to PDF or PNG

Zero values use API defaults. Use `ConvertToURL` for hosted output.

```go
label, err := client.Convert(ctx, zpljet.ConvertRequest{
    ZPL:      zpl,
    Format:   zpljet.FormatPNG,
    DPMM:     12,
    WidthMm:  101.6,
    HeightMm: 152.4,
})
if err != nil {
    log.Fatal(err)
}

label.Data        // []byte — the file
label.ContentType // "application/pdf" | "image/png"
label.ID          // conversion id (shows up in your dashboard)
```

### Hosted URLs (paid plans)

`ConvertToURL` returns a hosted public link. Retention follows the account
setting and plan limit.

```go
hosted, err := client.ConvertToURL(ctx, zpljet.ConvertRequest{ZPL: zpl})
if err != nil {
    log.Fatal(err)
}

hosted.URL           // public URL to the PDF (works until the file is deleted)
hosted.Pages         // pages rendered (one per ^XA…^XZ block)
hosted.RetentionDays // how long the file is kept
hosted.ExpiresAt     // time.Time — when the file is deleted and the URL stops working (UTC)
```

### Error handling

API errors are `*zpljet.Error` with a stable `Code` and typed context
fields; transport failures are `*zpljet.ConnError`. Branch with `errors.As`:

```go
_, err := client.Convert(ctx, zpljet.ConvertRequest{ZPL: zpl})
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
            log.Printf("rate limited, retry after %s", apiErr.RetryAfter)
        case zpljet.CodeConversionFailed:
            log.Printf("engine rejected the ZPL (conversion %s)", apiErr.ConversionID)
        }
    case errors.As(err, &connErr):
        log.Printf("network problem (timeout=%v): %v", connErr.Timeout(), connErr)
    default:
        log.Print(err)
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

Rate limits, transient 5xx responses, timeouts, and network failures retry up
to twice by default. Retries use exponential backoff and honor `Retry-After`.
`conversion_failed` is never retried.

### Configuration

```go
client, err := zpljet.New("zpl_…",
    zpljet.WithBaseURL("https://api.zpljet.com"),
    zpljet.WithTimeout(10*time.Second),
    zpljet.WithMaxRetries(5),
    zpljet.WithHTTPClient(customHTTPClient),
)
if err != nil {
    log.Fatal(err)
}
```

Cancellation and deadlines use standard `context.Context`; a caller-cancelled
context is returned as-is (never retried), a per-attempt timeout surfaces as
a `*ConnError` with `Timeout() == true` after retries.

## Examples

Runnable examples live in [`examples/`](https://github.com/zpljet/zpljet-go/tree/main/examples):

```sh
ZPLJET_API_KEY=zpl_… go run ./examples/convert-to-pdf
```

## Contributing & development

```sh
go vet ./...
go test ./...

ZPLJET_API_KEY=zpl_… go test ./tests -run E2E -v
```

## License

[MIT](https://github.com/zpljet/zpljet-go/blob/main/LICENSE)
