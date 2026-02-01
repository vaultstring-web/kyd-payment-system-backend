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
	receiverEmail := fmt.Sprintf("receiver-seed-%d@test.com", time.Now().UnixNano())
	receiverPass := "Password123!"
	receiverToken, _ := registerAndLogin(receiverEmail, receiverPass, "CN")
	receiverWalletNum := createWallet(receiverToken, "CNY")
	log.Printf("Receiver Wallet: %s (CNY)", receiverWalletNum)

	// 3. Setup Sender (MW)
	log.Println("--- Setting up Sender ---")
	senderEmail := fmt.Sprintf("admin-seed-%d@test.com", time.Now().UnixNano())
	senderPass := "Password123!"
	senderToken, senderID := registerAndLogin(senderEmail, senderPass, "MW")
	senderWalletNum := createWallet(senderToken, "MWK")
	log.Printf("Sender Wallet: %s (MWK)", senderWalletNum)

	// 4. Seed Balance (High Amount) & KYC
	log.Println("--- Seeding Balance & KYC ---")
	_, err = db.Exec(`
		UPDATE customer_schema.wallets 
		SET available_balance = available_balance + 10000000, ledger_balance = ledger_balance + 10000000 
		WHERE wallet_address = $1`, senderWalletNum)
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(`
		UPDATE customer_schema.users 
		SET kyc_level = 3, kyc_status = 'verified', email_verified = true 
		WHERE id = $1`, senderID)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Seeded KYC=Level3 and Balance=+10,000,000")

	// 5. Create Pending Transaction (600,000 MWK)
	log.Println("--- Creating Pending Transaction (600,000 MWK) ---")
	// RISK_ADMIN_APPROVAL_THRESHOLD is 500,000
	txID1 := makePayment(senderToken, receiverWalletNum, 600000)
	log.Printf("Created Transaction 1 (Should be Pending Approval): %s", txID1)

	// 6. Create Flagged Transaction (Manual Flag via DB)
	log.Println("--- Creating Flagged Transaction (1,000 MWK) ---")
	txID2 := makePayment(senderToken, receiverWalletNum, 1000)
	log.Printf("Created Transaction 2 (Normal): %s", txID2)

	// Flag it in DB
	_, err = db.Exec(`
		UPDATE customer_schema.transactions 
		SET metadata = jsonb_build_object('flagged', 'true', 'flag_reason', 'Suspicious activity detected by seed script') 
		WHERE id = $1`, txID2)
	if err != nil {
		log.Fatalf("Failed to flag tx: %v", err)
	}
	log.Println("Flagged Tx 2 in DB for Risk Alert")

	log.Println("--- Seed Complete ---")
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
		"first_name":   "Seed",
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

func makePayment(token, receiverWalletNum string, amount float64) string {
	ref := fmt.Sprintf("seed-ref-%d", time.Now().UnixNano())

	payload := map[string]interface{}{
		"amount":                 amount,
		"currency":               "MWK",
		"destination_currency":   "CNY",
		"receiver_wallet_number": receiverWalletNum,
		"description":            "Seed Payment",
		"channel":                "web",
		"category":               "transfer",
		"reference":              ref,
		"device_id":              "seed-device",
		"device_country":         "MW",
	}

	resp := request("POST", "/payments", token, payload)
	body := readBody(resp)
	
	// Accept 200 or 201
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		log.Fatalf("Payment failed: %d %s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(body), &result)
	
	if tx, ok := result["transaction"].(map[string]interface{}); ok {
		return tx["id"].(string)
	}
	return ""
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
	u, _ := url.Parse(baseURL)
	cookies := httpClient.Jar.Cookies(u)
	for _, c := range cookies {
		if c.Name == "csrf_token" {
			return c.Value
		}
	}

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
	resp.Body = io.NopCloser(bytes.NewBuffer(b))
	return string(b)
}
