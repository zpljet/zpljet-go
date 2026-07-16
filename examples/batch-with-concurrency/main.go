// Convert a batch of labels with a small concurrency limit so you stay under
// your plan's per-second rate limit (the SDK still auto-retries any 429s).
//
// Run: ZPLJET_API_KEY=zpl_… go run ./examples/batch-with-concurrency
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	zpljet "github.com/zpljet/zpljet-go"
)

func main() {
	client, err := zpljet.New(os.Getenv("ZPLJET_API_KEY"), zpljet.WithMaxRetries(5))
	if err != nil {
		log.Fatal(err)
	}

	orders := []string{"A-1001", "A-1002", "A-1003", "A-1004", "A-1005", "A-1006"}
	const concurrency = 2 // match your plan's rate limit

	var wg sync.WaitGroup
	limiter := make(chan struct{}, concurrency)
	for _, orderID := range orders {
		wg.Add(1)
		go func(orderID string) {
			defer wg.Done()
			limiter <- struct{}{}
			defer func() { <-limiter }()

			label, err := client.Convert(context.Background(), zpljet.ConvertRequest{
				ZPL: fmt.Sprintf(
					"^XA^FO40,40^A0N,50,50^FDOrder %s^FS^FO40,120^BY3^BCN,100,Y,N,N^FD%s^FS^XZ",
					orderID, orderID),
			})
			if err != nil {
				log.Printf("✗ %s: %v", orderID, err)
				return
			}
			if err := os.WriteFile(orderID+".pdf", label.Data, 0o644); err != nil {
				log.Printf("✗ %s: %v", orderID, err)
				return
			}
			fmt.Printf("✓ %s.pdf\n", orderID)
		}(orderID)
	}
	wg.Wait()

	fmt.Printf("Done — %d labels rendered.\n", len(orders))
}
