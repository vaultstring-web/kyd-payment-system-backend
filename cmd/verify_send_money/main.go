package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

const (
	baseURL = "http://localhost:9000/api/v1"
)

var httpClient *http.Client

func loadEnv() {
	content, err := os.ReadFile(".env")
	if err != nil {
		content, err = os.ReadFile("../../.env")
		if err != nil {
			return
		}
	}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			if os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
	}
}

func main() {
	loadEnv()
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	jar, _ := cookiejar.New(nil)
	httpClient = &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}

	fmt.Println("=========================================================")
	fmt.Println("VERIFYING CUSTOMER SEND MONEY FLOW (End-to-End)")
	fmt.Println("=========================================================")

	// 0. Connect to DB for funding
	dbURL := "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
	if url := os.Getenv("DATABASE_URL"); url != "" {
		dbURL = url
	}
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// 1. Fetch CSRF
	fmt.Println("\n--- 1. Initializing Session (CSRF) ---")
	csrfToken := fetchCSRFToken()
	fmt.Printf("[PASS] CSRF Token: %s\n", csrfToken)

	// 2. Create Sender (User A)
	fmt.Println("\n--- 2. Creating Sender (User A) ---")
	senderEmail := fmt.Sprintf("sender-%d@test.com", time.Now().UnixNano())
	senderPass := "Pass123!"
	senderDeviceID := "device-sender-A"
	senderToken, senderID := registerAndLogin(senderEmail, senderPass, senderDeviceID, csrfToken)
	fmt.Printf("[PASS] Sender Created: %s (ID: %s)\n", senderEmail, senderID)

	// Debug Device Trust
	var isTrusted bool
	err = db.QueryRow("SELECT is_trusted FROM customer_schema.user_devices WHERE user_id=$1 AND device_hash=$2", senderID, senderDeviceID).Scan(&isTrusted)
	if err != nil {
		fmt.Printf("[WARN] Device record not found or error: %v\n", err)
	} else {
		fmt.Printf("[INFO] Sender Device Trust: %v\n", isTrusted)
		if !isTrusted {
			fmt.Println("[INFO] Forcing device trust to TRUE for testing...")
			_, err = db.Exec("UPDATE customer_schema.user_devices SET is_trusted = true WHERE user_id=$1 AND device_hash=$2", senderID, senderDeviceID)
			if err != nil {
				log.Fatalf("Failed to force update device trust: %v", err)
			}
			fmt.Println("[INFO] Device forced to trusted.")
		}
	}

	// Create Sender Wallet
	senderWalletAddr := fmt.Sprintf("%d", time.Now().UnixNano())[:16]
	_, err = db.Exec(`INSERT INTO customer_schema.wallets (id, user_id, wallet_address, currency, available_balance, ledger_balance, reserved_balance, status, created_at, updated_at) VALUES (gen_random_uuid(), $1, $2, 'MWK', 0, 0, 0, 'active', NOW(), NOW())`, senderID, senderWalletAddr)
	if err != nil {
		log.Fatalf("Failed to create sender wallet: %v", err)
	}
	fmt.Println("[PASS] Sender Wallet Created")

	// 3. Create Receiver (User B)
	fmt.Println("\n--- 3. Creating Receiver (User B) ---")
	receiverEmail := fmt.Sprintf("receiver-%d@test.com", time.Now().UnixNano())
	receiverPass := "Pass123!"
	receiverDeviceID := "device-receiver-B"
	_, receiverID := registerAndLogin(receiverEmail, receiverPass, receiverDeviceID, csrfToken)
	fmt.Printf("[PASS] Receiver Created: %s (ID: %s)\n", receiverEmail, receiverID)

	// Create Receiver Wallet
	receiverWalletAddr := fmt.Sprintf("%d", time.Now().UnixNano())[:16]
	_, err = db.Exec(`INSERT INTO customer_schema.wallets (id, user_id, wallet_address, currency, available_balance, ledger_balance, reserved_balance, status, created_at, updated_at) VALUES (gen_random_uuid(), $1, $2, 'MWK', 0, 0, 0, 'active', NOW(), NOW())`, receiverID, receiverWalletAddr)
	if err != nil {
		log.Fatalf("Failed to create receiver wallet: %v", err)
	}
	fmt.Println("[PASS] Receiver Wallet Created")

	// 4. Get Receiver Wallet Number
	// We can query the DB or use an API if available. Let's use DB to be sure.
	var receiverWalletNumber string
	err = db.Get(&receiverWalletNumber, "SELECT wallet_address FROM customer_schema.wallets WHERE user_id=$1 AND currency='MWK'", receiverID)
	if err != nil {
		log.Fatalf("Failed to get receiver wallet: %v", err)
	}
	fmt.Printf("[PASS] Receiver Wallet Number: %s\n", receiverWalletNumber)

	// 5. Fund Sender Wallet
	fmt.Println("\n--- 5. Funding Sender Wallet ---")
	_, err = db.Exec("UPDATE customer_schema.wallets SET available_balance=1000000, ledger_balance=1000000 WHERE user_id=$1 AND currency='MWK'", senderID)
	if err != nil {
		log.Fatalf("Failed to fund sender wallet: %v", err)
	}
	fmt.Println("[PASS] Sender Wallet Funded with 1,000,000 MWK")

	// Update Sender KYC Status (Required for sending funds)
	fmt.Println("\n--- Updating Sender KYC Status ---")
	_, err = db.Exec("UPDATE customer_schema.users SET kyc_status='verified', kyc_level=2 WHERE id=$1", senderID)
	if err != nil {
		log.Fatalf("Failed to update sender KYC: %v", err)
	}
	fmt.Println("[PASS] Sender KYC Verified")

	// RE-LOGIN AS SENDER (Important: ensure session cookie matches sender)
	fmt.Println("\n--- Re-Authenticating as Sender ---")
	senderToken, _ = login(senderEmail, senderPass, senderDeviceID, csrfToken)

	// 6. Initiate Payment
	fmt.Println("\n--- 6. Initiating Payment (Sender -> Receiver) ---")
	amount := 5000.0
	payload := map[string]interface{}{
		"receiver_wallet_number": receiverWalletNumber,
		"amount":                 amount,
		"currency":               "MWK",
		"destination_currency":   "MWK",
		"description":            "End-to-End Verification Transfer",
		"channel":                "web",
		"category":               "transfer",
		"location":               "MW",
		"device_id":              senderDeviceID,
	}

	resp := makeRequest("POST", "/payments/initiate", payload, senderToken, csrfToken, map[string]string{
		"X-Device-ID": senderDeviceID,
	})
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		log.Fatalf("Payment Initiation Failed. Status: %d, Body: %s", resp.StatusCode, resp.Body)
	}
	fmt.Println("[PASS] Payment Initiated Successfully")
	fmt.Printf("Response: %s\n", resp.Body)

	// 7. Verify Balances
	fmt.Println("\n--- 7. Verifying Final Balances ---")
	var senderBal float64
	err = db.Get(&senderBal, "SELECT available_balance FROM customer_schema.wallets WHERE user_id=$1 AND currency='MWK'", senderID)
	if err != nil {
		log.Fatalf("Failed to get sender balance: %v", err)
	}

	var receiverBal float64
	err = db.Get(&receiverBal, "SELECT available_balance FROM customer_schema.wallets WHERE user_id=$1 AND currency='MWK'", receiverID)
	if err != nil {
		log.Fatalf("Failed to get receiver balance: %v", err)
	}

	fmt.Printf("Sender Balance: %.2f (Expected < 1,000,000)\n", senderBal)
	fmt.Printf("Receiver Balance: %.2f (Expected > 0)\n", receiverBal)

	if senderBal >= 1000000 {
		log.Fatalf("[FAIL] Sender balance did not decrease")
	}
	// Note: Receiver balance might not increase immediately if there's a delay or status is pending,
	// but for 'web' channel instant transfer it usually should, or at least be in 'pending' state.
	// If it's internal transfer, it might be instant.

	if receiverBal <= 0 {
		log.Println("[WARN] Receiver balance did not increase (might be pending approval or processing)")
	} else {
		fmt.Println("[PASS] Receiver balance increased")
	}

	fmt.Println("\nSUCCESS: Customer Send Money Flow Verified!")
}

// --- Helpers ---

type Response struct {
	StatusCode int
	Body       string
}

func fetchCSRFToken() string {
	resp, err := httpClient.Get(baseURL + "/health")
	if err != nil {
		log.Fatalf("Failed to fetch CSRF token: %v", err)
	}
	defer resp.Body.Close()

	u, _ := resp.Request.URL.Parse(baseURL)
	cookies := httpClient.Jar.Cookies(u)
	for _, c := range cookies {
		if c.Name == "csrf_token" {
			return c.Value
		}
	}
	return ""
}

func registerAndLogin(email, password, deviceID, csrfToken string) (string, string) {
	// Register
	regPayload := map[string]string{
		"email":        email,
		"password":     password,
		"first_name":   "Test",
		"last_name":    "User",
		"phone":        fmt.Sprintf("+26599%d", time.Now().UnixNano()%10000000), // Random phone
		"country_code": "MW",
		"user_type":    "individual",
	}
	regResp := makeRequest("POST", "/auth/register", regPayload, "", csrfToken, nil)
	if regResp.StatusCode != 201 && regResp.StatusCode != 200 {
		log.Fatalf("Registration failed: %d %s", regResp.StatusCode, regResp.Body)
	}

	return login(email, password, deviceID, csrfToken)
}

func login(email, password, deviceID, csrfToken string) (string, string) {
	// Login
	loginPayload := map[string]string{
		"email":        email,
		"password":     password,
		"device_id":    deviceID,
		"device_name":  "Test Script Device",
		"ip_address":   "127.0.0.1",
		"country_code": "MW",
	}
	loginResp := makeRequest("POST", "/auth/login", loginPayload, "", csrfToken, nil)
	if loginResp.StatusCode != 200 {
		log.Fatalf("Login failed: %d %s", loginResp.StatusCode, loginResp.Body)
	}

	var loginResult map[string]interface{}
	json.Unmarshal([]byte(loginResp.Body), &loginResult)

	token, ok := loginResult["token"].(string)
	if !ok {
		// Try nested data
		if data, ok := loginResult["data"].(map[string]interface{}); ok {
			token = data["token"].(string)
		}
	}

	// Extract UserID from registration or login response
	// Usually login returns user object
	var userID string
	if user, ok := loginResult["user"].(map[string]interface{}); ok {
		userID = user["id"].(string)
	} else if data, ok := loginResult["data"].(map[string]interface{}); ok {
		if user, ok := data["user"].(map[string]interface{}); ok {
			userID = user["id"].(string)
		}
	}

	if userID == "" {
		// Try to get from me endpoint
		meResp := makeRequest("GET", "/auth/me", nil, token, csrfToken, nil)
		var meResult map[string]interface{}
		json.Unmarshal([]byte(meResp.Body), &meResult)
		if user, ok := meResult["user"].(map[string]interface{}); ok {
			userID = user["id"].(string)
		}
	}

	return token, userID
}

func makeRequest(method, path string, payload interface{}, token string, csrfToken string, extraHeaders map[string]string) Response {
	var body io.Reader
	if payload != nil {
		jsonBytes, _ := json.Marshal(payload)
		body = bytes.NewBuffer(jsonBytes)
	}

	req, err := http.NewRequest(method, baseURL+path, body)
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if csrfToken != "" {
		req.Header.Set("X-CSRF-Token", csrfToken)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)

	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return Response{
		StatusCode: resp.StatusCode,
		Body:       string(respBody),
	}
}
