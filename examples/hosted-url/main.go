// Host the rendered PDF and get a public URL back (paid plans).
//
// Run: ZPLJET_API_KEY=zpl_… go run ./examples/hosted-url
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

	hosted, err := client.ConvertToURL(context.Background(), zpljet.ConvertRequest{
		ZPL: "^XA^FO50,50^A0N,50,50^FDHosted label^FS^XZ",
	})
	if err != nil {
		var apiErr *zpljet.Error
		if errors.As(err, &apiErr) && (apiErr.Code == zpljet.CodeHostingNotAllowed ||
			apiErr.Code == zpljet.CodeNoRetentionEnforced) {
			fmt.Println("Hosting not available on this plan:", apiErr.Message)
			return
		}
		log.Fatal(err)
	}

	fmt.Println("URL:     ", hosted.URL)
	fmt.Println("Pages:   ", hosted.Pages)
	fmt.Printf("Retained: %d days (deleted %s)\n", hosted.RetentionDays, hosted.ExpiresAt)
}
