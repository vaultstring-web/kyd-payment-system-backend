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

	// Setup HTTP Client
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
	receiverWalletNum := createWallet(receiverToken, "CNY")

	// 3. Setup Sender (MW)
	log.Println("--- Setting up Sender ---")
	senderEmail := fmt.Sprintf("sender-%d@test.com", time.Now().UnixNano())
	senderPass := "Password123!"
	senderToken, senderID := registerAndLogin(senderEmail, senderPass, "MW")
	senderWalletNum := createWallet(senderToken, "MWK")

	// 4. Seed Data (KYC & Balance)
	log.Println("--- Seeding Data via DB ---")
	seedData(db, senderID)

	// 5. Output Credentials
	fmt.Println("\n============================================")
	fmt.Println("FRONTEND TEST CREDENTIALS")
	fmt.Println("============================================")
	fmt.Printf("Sender Email:    %s\n", senderEmail)
	fmt.Printf("Sender Password: %s\n", senderPass)
	fmt.Printf("Sender Wallet:   %s\n", senderWalletNum)
	fmt.Println("--------------------------------------------")
	fmt.Printf("Receiver Wallet: %s\n", receiverWalletNum)
	fmt.Println("============================================")
}

// --- Helper Functions ---

func registerAndLogin(email, password, country string) (string, string) {
	var phone string
	if country == "CN" {
		phone = fmt.Sprintf("+86138%08d", time.Now().UnixNano()%100000000)
	} else {
		phone = fmt.Sprintf("+26599%07d", time.Now().UnixNano()%10000000)
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

	var res map[string]interface{}
	json.Unmarshal([]byte(readBody(resp)), &res)
	token := res["access_token"].(string)

	// Get User ID
	resp = request("GET", "/auth/me", token, nil)
	body := readBody(resp)
	// log.Println("/auth/me response:", body) // Debug
	var meRes map[string]interface{}
	json.Unmarshal([]byte(body), &meRes)

	// Handle different response structures
	var userID string
	if data, ok := meRes["data"].(map[string]interface{}); ok {
		userID = data["id"].(string)
	} else if id, ok := meRes["id"].(string); ok {
		userID = id
	} else {
		log.Fatalf("Could not find user ID in /auth/me response: %s", body)
	}

	return token, userID
}

func createWallet(token, currency string) string {
	payload := map[string]string{"currency": currency}
	resp := request("POST", "/wallets", token, payload)
	if resp.StatusCode != 201 {
		// If wallet exists (409), fetch it
		if resp.StatusCode == 409 {
			resp = request("GET", "/wallets", token, nil)
		} else {
			log.Fatalf("Create wallet failed: %d %s", resp.StatusCode, readBody(resp))
		}
	}

	body := readBody(resp)
	var res map[string]interface{}
	json.Unmarshal([]byte(body), &res)

	// Handle list or single object
	if wallets, ok := res["wallets"].([]interface{}); ok {
		for _, w := range wallets {
			wm := w.(map[string]interface{})
			if wm["currency"] == currency {
				return wm["wallet_address"].(string) // Use wallet_address (16 digits)
			}
		}
	} else if data, ok := res["data"].(map[string]interface{}); ok {
		return data["wallet_address"].(string)
	}

	// Fallback for direct response
	if addr, ok := res["wallet_address"].(string); ok {
		return addr
	}

	log.Fatalf("Could not find wallet address in response: %s", body)
	return ""
}

func seedData(db *sqlx.DB, userID string) {
	// Verify Email
	_, err := db.Exec(`
		UPDATE customer_schema.users 
		SET email_verified = true, kyc_status = 'verified' 
		WHERE id = $1
	`, userID)
	if err != nil {
		log.Fatalf("Failed to verify user: %v", err)
	}

	// Fund Wallet
	_, err = db.Exec(`
		UPDATE customer_schema.wallets 
		SET available_balance = available_balance + 50000, 
		    ledger_balance = ledger_balance + 50000 
		WHERE user_id = $1 AND currency = 'MWK'
	`, userID)
	if err != nil {
		log.Fatalf("Failed to fund wallet: %v", err)
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
	}

	// Add CSRF Token
	u, _ := url.Parse(baseURL)
	cookies := httpClient.Jar.Cookies(u)
	for _, cookie := range cookies {
		if cookie.Name == "csrf_token" {
			req.Header.Set("X-CSRF-Token", cookie.Value)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	return resp
}

func readBody(resp *http.Response) string {
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}
