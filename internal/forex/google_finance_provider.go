package forex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kyd/internal/domain"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// GoogleFinanceProvider fetches rates by scraping Google Finance.
type GoogleFinanceProvider struct {
	client *http.Client
}

func NewGoogleFinanceProvider() *GoogleFinanceProvider {
	return &GoogleFinanceProvider{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *GoogleFinanceProvider) Name() string {
	return "GoogleFinance"
}

func (p *GoogleFinanceProvider) GetRate(ctx context.Context, from, to domain.Currency) (*domain.ExchangeRate, error) {
	// 1. Construct URL
	// Google Finance format: https://www.google.com/finance/quote/MWK-CNY
	url := fmt.Sprintf("https://www.google.com/finance/quote/%s-%s", from, to)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 2. Set headers to mimic a browser (essential for scraping)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	// 3. Execute request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google finance returned status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}
	body := string(bodyBytes)

	// 4. Extract rate using Regex
	// The class "YMlKec fxKbKc" is commonly used for the big price/rate number.
	// We look for: <div class="YMlKec fxKbKc">...</div>
	// Value might contain commas (e.g. 1,234.56)
	rePrice := regexp.MustCompile(`<div class="YMlKec fxKbKc">([0-9.,]+)</div>`)
	matchesPrice := rePrice.FindStringSubmatch(body)

	if len(matchesPrice) < 2 {
		return nil, fmt.Errorf("could not find rate in HTML")
	}

	rateStr := matchesPrice[1]
	rateStr = strings.ReplaceAll(rateStr, ",", "")
	rateVal, err := strconv.ParseFloat(rateStr, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rate value '%s': %w", rateStr, err)
	}

	// 5. Extract Change and Percentage via Previous Close
	// If direct change text is hidden, we use "Previous close" to calculate it.
	// Regex looks for "Previous close" followed by a value in a div.
	// Pattern based on observation: Previous close</div>...<div class="P6K39c">0.84</div>
	// We use a broader regex to be safe: Previous close</div>.*?<div[^>]*>([0-9.,]+)</div>
	rePrevClose := regexp.MustCompile(`Previous close</div>.*?<div[^>]*>([0-9.,]+)</div>`)
	matchesPrev := rePrevClose.FindStringSubmatch(body)

	var change24h, changePercent float64

	if len(matchesPrev) >= 2 {
		prevStr := strings.ReplaceAll(matchesPrev[1], ",", "")
		prevClose, err := strconv.ParseFloat(prevStr, 64)
		if err == nil && prevClose != 0 {
			change24h = rateVal - prevClose
			changePercent = (change24h / prevClose) * 100
		}
	} else {
		// Fallback: Look for patterns like "+0.050 (+0.45%)" if Previous Close fails
		reChange := regexp.MustCompile(`([+-][0-9.,]+)\s*\(\s*([+-][0-9.,]+)%\s*\)`)
		matchesChange := reChange.FindStringSubmatch(body)
		if len(matchesChange) >= 3 {
			cStr := strings.ReplaceAll(matchesChange[1], ",", "")
			pStr := strings.ReplaceAll(matchesChange[2], ",", "")
			change24h, _ = strconv.ParseFloat(cStr, 64)
			changePercent, _ = strconv.ParseFloat(pStr, 64)
		}
	}

	// 6. Extract Day Range (High/Low)
	// Look for "Day Range" followed by "Low - High"
	// Pattern: >Day Range<... >1,234.56 - 1,245.67<
	reRange := regexp.MustCompile(`Day Range</div>[^<]*<div[^>]*>\s*([0-9.,]+)\s*-\s*([0-9.,]+)`)
	matchesRange := reRange.FindStringSubmatch(body)

	var low24h, high24h float64
	if len(matchesRange) >= 3 {
		lStr := strings.ReplaceAll(matchesRange[1], ",", "")
		hStr := strings.ReplaceAll(matchesRange[2], ",", "")
		low24h, _ = strconv.ParseFloat(lStr, 64)
		high24h, _ = strconv.ParseFloat(hStr, 64)
	} else {
		// Fallback if range not found: use current rate +/- tiny margin or set to current
		low24h = rateVal
		high24h = rateVal
	}

	// 7. Return ExchangeRate
	return &domain.ExchangeRate{
		ID:             uuid.New(),
		BaseCurrency:   from,
		TargetCurrency: to,
		Rate:           decimal.NewFromFloat(rateVal),
		Source:         p.Name(),
		ValidFrom:      time.Now(),
		LastUpdated:    time.Now(),

		// New Stats
		Change24h:     decimal.NewFromFloat(change24h),
		ChangePercent: decimal.NewFromFloat(changePercent),
		High24h:       decimal.NewFromFloat(high24h),
		Low24h:        decimal.NewFromFloat(low24h),
	}, nil
}
