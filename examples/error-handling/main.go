// Handle every error the API can return, with typed context fields.
//
// Run: ZPLJET_API_KEY=zpl_… go run ./examples/error-handling
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	zpljet "github.com/zpljet/zpljet-go"
)

func main() {
	client, err := zpljet.New(os.Getenv("ZPLJET_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	// Deliberately invalid — there is no ^XA…^XZ block.
	_, err = client.Convert(context.Background(), zpljet.ConvertRequest{ZPL: "this is not zpl"})
	if err == nil {
		fmt.Println("unexpectedly succeeded")
		return
	}

	var apiErr *zpljet.Error
	var connErr *zpljet.ConnError
	switch {
	case errors.As(err, &apiErr):
		switch apiErr.Code {
		case zpljet.CodeInvalidRequest:
			fmt.Printf("Invalid request — field %q: %s\nDocs: %s\n",
				apiErr.Param, apiErr.Message, apiErr.DocURL)
		case zpljet.CodeMissingAPIKey, zpljet.CodeInvalidAPIKey:
			fmt.Println("Bad API key — create one at https://zpljet.com/dashboard")
		case zpljet.CodeQuotaExceeded:
			fmt.Printf("Quota: %d/%d used, resets %s\n", apiErr.Used, apiErr.Quota, apiErr.ResetsAt)
		case zpljet.CodeRateLimitExceeded:
			// The SDK already retried with backoff before returning this.
			fmt.Printf("Still rate-limited — retry after %s (%s)\n", apiErr.RetryAfter, apiErr.RetryAt)
		case zpljet.CodeConversionFailed:
			fmt.Println("Engine rejected the ZPL — support id:", apiErr.ConversionID)
		case zpljet.CodeServiceUnavailable:
			fmt.Println("Render engine temporarily unavailable — retry after:", apiErr.RetryAfter)
		default:
			fmt.Println("API error:", apiErr)
		}
	case errors.As(err, &connErr):
		fmt.Println("Network/timeout problem after retries:", connErr)
	default:
		log.Fatal(err) // context cancellation, programming error, …
	}
}
