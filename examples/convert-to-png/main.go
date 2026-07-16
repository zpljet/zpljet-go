// Render a 300 dpi PNG preview of a 4x6" shipping label.
//
// Run: ZPLJET_API_KEY=zpl_… go run ./examples/convert-to-png
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
		ZPL: "^XA" +
			"^FO40,40^A0N,60,60^FDACME Logistics^FS" +
			"^FO40,130^BY3^BCN,120,Y,N,N^FD123456789012^FS" +
			"^XZ",
		Format:   zpljet.FormatPNG,
		DPMM:     12, // 300 dpi
		WidthMm:  101.6,
		HeightMm: 152.4,
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile("label.png", label.Data, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Saved label.png (%d bytes)\n", len(label.Data))
}
