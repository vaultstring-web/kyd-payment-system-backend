package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"kyd/internal/domain"
	"kyd/internal/forex"
)

func main() {
	provider := forex.NewGoogleFinanceProvider()

	pairs := []struct {
		From domain.Currency
		To   domain.Currency
	}{
		{"USD", "MWK"},
		{"USD", "EUR"},
		{"GBP", "USD"},
		{"EUR", "USD"},
		{"MWK", "CNY"}, // Cross-rate via USD usually, but Google might have direct
	}

	fmt.Println("Verifying Google Finance Scraper...")
	fmt.Println("-----------------------------------")

	for _, pair := range pairs {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		start := time.Now()
		rate, err := provider.GetRate(ctx, pair.From, pair.To)
		duration := time.Since(start)
		cancel()

		if err != nil {
			log.Printf("❌ %s -> %s: Failed (%v)\n", pair.From, pair.To, err)
			continue
		}

		fmt.Printf("✅ %s -> %s: %s (Source: %s)\n", rate.BaseCurrency, rate.TargetCurrency, rate.Rate.String(), rate.Source)
		fmt.Printf("   Details: High: %s, Low: %s, Change: %s (%s%%)\n", 
			rate.High24h.String(), 
			rate.Low24h.String(), 
			rate.Change24h.String(), 
			rate.ChangePercent.String())
		fmt.Printf("   Latency: %v\n", duration)
		fmt.Println("-----------------------------------")
	}
}
