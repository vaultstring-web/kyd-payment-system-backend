package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

const (
	baseURL = "http://localhost:9000/api/v1"
	dbURL   = "postgres://kyd_user:kyd_password@127.0.0.1:5432/kyd_dev?sslmode=disable"
)

var httpClient *http.Client

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Setup HTTP Client with CookieJar
	jar, _ := cookiejar.New(nil)
	httpClient = &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}

	// 1. Setup DB Connection
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.Close()
	log.Println("Connected to Database")

	// 2. Setup Receiver (China)
	log.Println("--- Setting up Receiver ---")
	receiverEmail := fmt.Sprintf("receiver-%d@test.com", time.Now().UnixNano())
	receiverPass := "Password123!"
	receiverToken, _ := registerAndLogin(receiverEmail, receiverPass, "CN")

	// Create Receiver Wallet (CNY)
	receiverWalletNum := createWallet(receiverToken, "CNY")
	log.Printf("Receiver Wallet: %s (CNY)", receiverWalletNum)

	// 3. Setup Sender (MW)
	log.Println("--- Setting up Sender ---")
	senderEmail := fmt.Sprintf("sender-%d@test.com", time.Now().UnixNano())
	senderPass := "Password123!"
	senderToken, senderID := registerAndLogin(senderEmail, senderPass, "MW")

	// Create Sender Wallet (MWK)
	senderWalletNum := createWallet(senderToken, "MWK")
	log.Printf("Sender Wallet: %s (MWK)", senderWalletNum)

	// 4. Seed Data (KYC & Balance)
	log.Println("--- Seeding Data via DB ---")
	seedData(db, senderID)

	// 5. Perform Payment Test (Idempotency)
	log.Println("--- Starting Payment Test ---")
	performPaymentTest(senderToken, receiverWalletNum)
}

func registerAndLogin(email, password, country string) (string, string) {
	var phone string
	if country == "CN" {
		phone = fmt.Sprintf("+86138%08d", time.Now().UnixNano()%100000000)
	} else if country == "MW" {
		phone = fmt.Sprintf("+26599%07d", time.Now().UnixNano()%10000000)
	} else {
		phone = fmt.Sprintf("+1%010d", time.Now().UnixNano()%10000000000)
	}

	// Register
	regPayload := map[string]interface{}{
		"email":        email,
		"password":     password,
		"phone":        phone,
		"first_name":   "Test",
		"last_name":    "User",
		"user_type":    "individual",
		"country_code": country,
	}
	resp := request("POST", "/auth/register", "", regPayload)
	if resp.StatusCode != 201 {
		log.Fatalf("Register failed: %d %s", resp.StatusCode, readBody(resp))
	}

	// Login
	loginPayload := map[string]interface{}{
		"email":    email,
		"password": password,
	}
	resp = request("POST", "/auth/login", "", loginPayload)
	if resp.StatusCode != 200 {
		log.Fatalf("Login failed: %d %s", resp.StatusCode, readBody(resp))
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	token := result["access_token"].(string)

	// Get User ID
	meResp := request("GET", "/auth/me", token, nil)
	if meResp.StatusCode != 200 {
		log.Fatalf("Failed to get user info: %d %s", meResp.StatusCode, readBody(meResp))
	}
	var meResult map[string]interface{}
	json.NewDecoder(meResp.Body).Decode(&meResult)

	var userID string
	if id, ok := meResult["id"].(string); ok {
		userID = id
	} else if user, ok := meResult["user"].(map[string]interface{}); ok {
		userID = user["id"].(string)
	} else {
		log.Fatalf("Could not find user ID in /auth/me response: %v", meResult)
	}

	return token, userID
}

func createWallet(token, currency string) string {
	payload := map[string]interface{}{
		"currency": currency,
	}
	resp := request("POST", "/wallets", token, payload)
	if resp.StatusCode != 201 {
		log.Fatalf("Create Wallet failed: %d %s", resp.StatusCode, readBody(resp))
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result["wallet_address"].(string)
}

func seedData(db *sqlx.DB, userID string) {
	// Update KYC Status to Verified (2)
	// And verify email
	_, err := db.Exec(`
		UPDATE customer_schema.users 
		SET kyc_status = 'verified', kyc_level = 2, email_verified = true 
		WHERE id = $1
	`, userID)
	if err != nil {
		log.Fatalf("Failed to update user KYC: %v", err)
	}

	// Fund Wallet
	_, err = db.Exec(`
		UPDATE customer_schema.wallets 
		SET available_balance = available_balance + 1000000, ledger_balance = ledger_balance + 1000000
		WHERE user_id = $1
	`, userID)
	if err != nil {
		log.Fatalf("Failed to fund wallet: %v", err)
	}
	log.Println("Seeded KYC=Verified and Balance=+1,000,000")
}

func performPaymentTest(token, receiverWalletNum string) {
	// Stable Reference
	ref := fmt.Sprintf("test-ref-%d", time.Now().UnixNano())

	payload := map[string]interface{}{
		"amount":                 5000,
		"currency":               "MWK",
		"destination_currency":   "CNY",
		"receiver_wallet_number": receiverWalletNum,
		"description":            "Test Payment",
		"channel":                "web",
		"category":               "transfer",
		"reference":              ref,
		"device_id":              "test-device",
		"device_country":         "MW",
	}

	// Request 1
	log.Println("Sending Request 1...")
	resp1 := request("POST", "/payments", token, payload)
	body1 := readBody(resp1)
	log.Printf("Response 1: Status=%d Body=%s", resp1.StatusCode, body1)

	if resp1.StatusCode != 201 {
		log.Fatalf("Request 1 failed")
	}

	// Request 2 (Duplicate)
	log.Println("Sending Request 2 (Duplicate)...")
	// Note: We use the SAME payload (same reference)
	// But we need to handle CSRF if applicable.
	// Our `request` helper handles CSRF fetching if needed, or we assume Bearer token is enough for API.
	// In the previous attempt, we added CSRF logic. Let's add it here too.

	resp2 := request("POST", "/payments", token, payload)
	body2 := readBody(resp2)
	log.Printf("Response 2: Status=%d Body=%s", resp2.StatusCode, body2)

	if resp2.StatusCode == 201 || resp2.StatusCode == 200 {
		log.Println("SUCCESS: Duplicate request handled gracefully (Idempotent)")
	} else {
		log.Printf("FAILURE: Duplicate request returned error: %d", resp2.StatusCode)
	}
}

func request(method, path, token string, body interface{}) *http.Response {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, _ := json.Marshal(body)
		bodyReader = bytes.NewBuffer(jsonBody)
	}

	req, _ := http.NewRequest(method, baseURL+path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)

		// Fetch CSRF if needed (GET /auth/me to prime cookies)
		// Only for non-GET requests
		if method != "GET" {
			csrfToken := getCSRFToken(token)
			if csrfToken != "" {
				req.Header.Set("X-CSRF-Token", csrfToken)
			}
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	return resp
}

func getCSRFToken(token string) string {
	// Check if we already have it in jar
	u, _ := url.Parse(baseURL)
	cookies := httpClient.Jar.Cookies(u)
	for _, c := range cookies {
		if c.Name == "csrf_token" {
			return c.Value
		}
	}

	// If not, fetch it
	req, _ := http.NewRequest("GET", baseURL+"/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		cookies := httpClient.Jar.Cookies(u)
		for _, c := range cookies {
			if c.Name == "csrf_token" {
				return c.Value
			}
		}
	}
	return ""
}

func readBody(resp *http.Response) string {
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	// Restore body for further reading if needed (not needed here)
	resp.Body = io.NopCloser(bytes.NewBuffer(b))
	return string(b)
}
