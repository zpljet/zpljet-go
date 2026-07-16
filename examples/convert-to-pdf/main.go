// Convert ZPL to a PDF and save it locally.
//
// Run: ZPLJET_API_KEY=zpl_… go run ./examples/convert-to-pdf
package main

import (
	"context"
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

	label, err := client.Convert(context.Background(), zpljet.ConvertRequest{
		ZPL: "^XA^FO50,50^A0N,50,50^FDHello from ZPLJet^FS^XZ",
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile("label.pdf", label.Data, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Saved label.pdf (%d bytes, conversion %s)\n", len(label.Data), label.ID)
}
