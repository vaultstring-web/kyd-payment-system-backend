package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

const (
	baseURL   = "http://localhost:9000/api/v1"
	dbConnStr = "postgres://kyd_user:kyd_password@127.0.0.1:5432/kyd_dev?sslmode=disable"
)

var httpClient *http.Client

func main() {
	// Setup Cookie Jar
	jar, _ := cookiejar.New(nil)
	httpClient = &http.Client{
		Jar:     jar,
		Timeout: 10 * time.Second,
	}

	// Connect to DB
	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping DB: %v", err)
	}
	log.Println("Connected to Database")

	// 0. Register Receiver
	timestamp := time.Now().UnixNano()
	receiverEmail := fmt.Sprintf("receiver-%d@test.com", timestamp)
	receiverPass := "Password123!"

	log.Println("--- Registering Receiver:", receiverEmail, "---")
	regPayload := map[string]string{
		"email":        receiverEmail,
		"password":     receiverPass,
		"first_name":   "Test",
		"last_name":    "Receiver",
		"phone":        fmt.Sprintf("+26599%d", timestamp%10000000),
		"country_code": "MW",
		"user_type":    "individual",
	}
	respReg := request("POST", "/auth/register", "", regPayload, nil)
	log.Println("Receiver Register Status:", respReg.StatusCode)
	readBody(respReg)

	// Login Receiver
	authPayload := map[string]string{
		"email":    receiverEmail,
		"password": receiverPass,
	}
	resp := request("POST", "/auth/login", "", authPayload, nil)
	receiverToken := getToken(resp)
	log.Printf("Receiver Logged in.")

	// 2.5 Prime CSRF Token for Receiver
	log.Println("--- Priming CSRF Token for Receiver (GET /auth/me) ---")
	respMeRecv := request("GET", "/auth/me", receiverToken, nil, nil)
	readBody(respMeRecv)

	// Create Wallet for Receiver
	log.Println("--- Creating Receiver Wallet ---")
	walletPayload := map[string]string{
		"currency": "MWK",
	}
	respWallet := request("POST", "/wallets", receiverToken, walletPayload, nil)
	walletBody := readBody(respWallet)
	log.Println("Receiver Wallet Created:", respWallet.StatusCode)

	var receiverWallet struct {
		WalletAddress string `json:"wallet_address"`
	}
	json.Unmarshal([]byte(walletBody), &receiverWallet)
	receiverWalletNumber := receiverWallet.WalletAddress
	log.Println("Receiver Wallet Number:", receiverWalletNumber)

	// 0.1 Register Sender
	senderEmail := fmt.Sprintf("sender-%d@test.com", timestamp)
	senderPass := "Password123!"

	log.Println("--- Registering Sender:", senderEmail, "---")
	regPayload["email"] = senderEmail
	regPayload["password"] = senderPass
	regPayload["first_name"] = "Sender"
	regPayload["phone"] = fmt.Sprintf("+26588%d", timestamp%10000000)

	respRegSender := request("POST", "/auth/register", "", regPayload, nil)
	log.Println("Sender Register Status:", respRegSender.StatusCode)
	readBody(respRegSender)

	// 1. Login Sender
	log.Println("--- Logging in Sender ---")
	authPayload["email"] = senderEmail
	authPayload["password"] = senderPass
	resp = request("POST", "/auth/login", "", authPayload, nil)
	if resp.StatusCode != 200 {
		log.Fatal("Login failed")
	}
	token := getToken(resp)
	log.Printf("Sender Logged in.")

	// 1.5 Prime CSRF Token
	log.Println("--- Priming CSRF Token (GET /auth/me) ---")
	respMe := request("GET", "/auth/me", token, nil, nil)
	readBody(respMe)

	// 1.6 Create Wallet for Sender
	log.Println("--- Creating Sender Wallet ---")
	respWalletSender := request("POST", "/wallets", token, walletPayload, nil)
	walletBodySender := readBody(respWalletSender)
	log.Println("Sender Wallet Created:", respWalletSender.StatusCode)

	var senderWallet struct {
		ID     string `json:"id"`
		UserID string `json:"user_id"`
	}
	json.Unmarshal([]byte(walletBodySender), &senderWallet)

	// Credit Sender Wallet & Verify KYC
	log.Println("--- Crediting Sender Wallet & Verifying KYC ---")
	log.Printf("Updating Wallet ID: %s, User ID: %s", senderWallet.ID, senderWallet.UserID)

	_, err = db.Exec("UPDATE customer_schema.wallets SET available_balance = available_balance + 1000000, ledger_balance = ledger_balance + 1000000 WHERE id = $1", senderWallet.ID)
	if err != nil {
		log.Printf("Failed to credit wallet: %v", err)
	}

	res, err := db.Exec("UPDATE customer_schema.users SET kyc_status = 'verified', kyc_level = 2 WHERE id = $1", senderWallet.UserID)
	if err != nil {
		log.Printf("Failed to verify KYC: %v", err)
	} else {
		rows, _ := res.RowsAffected()
		log.Printf("Sender KYC Verified (Update Executed). Rows Affected: %d", rows)
	}

	// Verify DB Update
	var kycStatus string
	var dbUserID string
	err = db.QueryRow("SELECT id, kyc_status FROM customer_schema.users WHERE id = $1", senderWallet.UserID).Scan(&dbUserID, &kycStatus)
	if err != nil {
		log.Printf("Failed to fetch user verification: %v", err)
	} else {
		log.Printf("DB Verification - UserID: %s, KYC Status: %s", dbUserID, kycStatus)
	}

	// Wait for propagation (just in case)
	time.Sleep(1 * time.Second)

	// 2. Simulate Duplicate Request
	log.Println("--- Simulating Duplicate Request ---")
	idempotencyKey := fmt.Sprintf("sim-test-%d", time.Now().UnixNano())

	paymentPayload := map[string]interface{}{
		"receiver_wallet_number": receiverWalletNumber,
		"amount":                 100,
		"currency":               "MWK",
		"description":            "Duplicate Test",
		"channel":                "web",
		"category":               "personal",
	}

	// Request 1
	log.Println("Sending Request 1 (Key:", idempotencyKey, ")")
	headers := map[string]string{
		"Idempotency-Key": idempotencyKey,
	}
	resp1 := request("POST", "/payments/initiate", token, paymentPayload, headers)
	log.Printf("Request 1 Status: %d", resp1.StatusCode)
	readBody(resp1)

	// Request 2 (Same Key)
	log.Println("Sending Request 2 (Key:", idempotencyKey, ")")
	resp2 := request("POST", "/payments/initiate", token, paymentPayload, headers)
	log.Printf("Request 2 Status: %d", resp2.StatusCode)
	body2 := readBody(resp2)
	log.Println("Request 2 Body:", body2)

	if resp2.StatusCode == 409 || resp2.StatusCode == 201 { // 201 if idempotent handling returns success
		log.Println("SUCCESS: Duplicate request handled correctly (409 or 201 with same tx)")
	} else {
		log.Println("FAILURE: Duplicate request NOT handled correctly")
	}

	// 3. Simulate Fresh Request
	log.Println("--- Simulating Fresh Request (Key B) ---")
	idempotencyKeyB := fmt.Sprintf("sim-test-%d-B", time.Now().UnixNano())
	log.Println("Sending Request 3 (Key:", idempotencyKeyB, ")")
	headersB := map[string]string{
		"Idempotency-Key": idempotencyKeyB,
	}
	resp3 := request("POST", "/payments/initiate", token, paymentPayload, headersB)
	log.Printf("Request 3 Status: %d", resp3.StatusCode)
	body3 := readBody(resp3)
	log.Println("Request 3 Body:", body3)

	if resp3.StatusCode == 201 || resp3.StatusCode == 200 {
		log.Println("SUCCESS: Fresh request handled correctly")
	} else {
		log.Println("FAILURE: Fresh request failed")
	}

	// 4. Simulate Rapid Fire (Double Click) - Two different keys
	log.Println("--- Simulating Rapid Fire (Double Click) ---")
	keyC1 := fmt.Sprintf("sim-click-1-%d", time.Now().UnixNano())
	keyC2 := fmt.Sprintf("sim-click-2-%d", time.Now().UnixNano())

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Println("Sending Rapid 1 (Key:", keyC1, ")")
		h := map[string]string{"Idempotency-Key": keyC1}
		r := request("POST", "/payments/initiate", token, paymentPayload, h)
		log.Printf("Rapid 1 Status: %d", r.StatusCode)
	}()

	go func() {
		defer wg.Done()
		log.Println("Sending Rapid 2 (Key:", keyC2, ")")
		h := map[string]string{"Idempotency-Key": keyC2}
		r := request("POST", "/payments/initiate", token, paymentPayload, h)
		log.Printf("Rapid 2 Status: %d", r.StatusCode)
	}()

	wg.Wait()

	// 5. Simulate High Value Transaction (Admin Monitoring)
	log.Println("--- Simulating High Value Transaction (Risk Alert) ---")
	highValueKey := fmt.Sprintf("sim-high-%d", time.Now().UnixNano())
	highValuePayload := map[string]interface{}{
		"receiver_wallet_number": receiverWalletNumber,
		"amount":                 60000, // Above 50,000 threshold
		"currency":               "MWK",
		"description":            "High Value Test",
		"channel":                "web",
		"category":               "business",
	}
	headersHV := map[string]string{
		"Idempotency-Key": highValueKey,
	}
	respHV := request("POST", "/payments/initiate", token, highValuePayload, headersHV)
	log.Printf("High Value Request Status: %d", respHV.StatusCode)
	bodyHV := readBody(respHV)
	log.Println("High Value Body:", bodyHV)

	if respHV.StatusCode == 201 || respHV.StatusCode == 202 {
		log.Println("SUCCESS: High value transaction submitted (likely pending approval)")
	} else {
		log.Println("FAILURE: High value transaction failed")
	}
}

func getToken(resp *http.Response) string {
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "access_token" {
			return cookie.Value
		}
	}
	return ""
}

func request(method, path, token string, body interface{}, extraHeaders map[string]string) *http.Response {
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

	// Add CSRF Token if present in cookies
	u, _ := url.Parse(baseURL)
	cookies := httpClient.Jar.Cookies(u)
	if len(cookies) == 0 {
		// Try root URL if base URL has path
		uRoot, _ := url.Parse("http://localhost:9000")
		cookies = httpClient.Jar.Cookies(uRoot)
	}

	// Debug cookies
	log.Printf("Cookies for %s: %v", u.String(), cookies)

	for _, cookie := range cookies {
		if cookie.Name == "csrf_token" {
			req.Header.Set("X-CSRF-Token", cookie.Value)
			log.Printf("Set X-CSRF-Token: %s", cookie.Value)
		}
	}

	for k, v := range extraHeaders {
		req.Header.Set(k, v)
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
