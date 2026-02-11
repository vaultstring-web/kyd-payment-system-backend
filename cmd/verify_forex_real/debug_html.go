package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

func debugHTML() {
	url := "https://www.google.com/finance/quote/USD-EUR"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	keywords := []string{"Previous close", "Day range", "Year range", "Range", "Previous"}
	for _, kw := range keywords {
		idx := strings.Index(body, kw)
		if idx != -1 {
			fmt.Printf("Found '%s' at index %d\n", kw, idx)
			start := idx - 50
			if start < 0 {
				start = 0
			}
			end := idx + 200
			if end > len(body) {
				end = len(body)
			}
			fmt.Println(body[start:end])
		} else {
			fmt.Printf("'%s' not found\n", kw)
		}
	}
}
