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

	// 2. Create Admin User
	log.Println("--- Creating Admin User ---")
	adminEmail := fmt.Sprintf("verify-admin-%d@test.com", time.Now().UnixNano())
	adminPass := "Password123!"

	// Register normal user
	token, userID := registerAndLogin(adminEmail, adminPass)
	log.Printf("Registered User: %s (ID: %s)", adminEmail, userID)

	// Promote to Admin via DB (using user_type='admin')
	_, err = db.Exec("UPDATE customer_schema.users SET user_type='admin', kyc_status='verified' WHERE id=$1", userID)
	if err != nil {
		log.Fatalf("Failed to promote user to admin: %v", err)
	}
	log.Println("Promoted user to ADMIN (user_type='admin')")

	// Re-login to ensure token has admin claims (if claims are baked in token)
	// or just use existing token if role is checked at runtime against DB (safer to re-login)
	token, _ = registerAndLogin(adminEmail, adminPass)

	// 3. Verify System Stats
	log.Println("--- Verifying System Stats ---")
	stats := request("GET", "/admin/system/stats", token, nil)
	if stats.StatusCode != 200 {
		log.Printf("FAILED: Get Stats %d %s", stats.StatusCode, readBody(stats))
	} else {
		log.Printf("SUCCESS: System Stats: %s", readBody(stats))
	}

	// 4. Verify Transaction Volume
	log.Println("--- Verifying Transaction Volume ---")
	volume := request("GET", "/admin/reports/volume?months=6", token, nil)
	if volume.StatusCode != 200 {
		log.Printf("FAILED: Get Volume %d %s", volume.StatusCode, readBody(volume))
	} else {
		log.Printf("SUCCESS: Transaction Volume: %s", readBody(volume))
	}

	// 5. Verify Risk Alerts
	log.Println("--- Verifying Risk Alerts ---")
	alerts := request("GET", "/admin/risk/alerts?limit=10&offset=0", token, nil)
	if alerts.StatusCode != 200 {
		log.Printf("FAILED: Get Alerts %d %s", alerts.StatusCode, readBody(alerts))
	} else {
		log.Printf("SUCCESS: Risk Alerts: %s", readBody(alerts))
	}

	// 6. Verify Users List (Merchant Filter)
	log.Println("--- Verifying Merchant List ---")
	merchants := request("GET", "/admin/users?limit=10&offset=0&type=merchant", token, nil)
	if merchants.StatusCode != 200 {
		log.Printf("FAILED: Get Merchants %d %s", merchants.StatusCode, readBody(merchants))
	} else {
		log.Printf("SUCCESS: Merchants: %s", readBody(merchants))
	}
}

func registerAndLogin(email, password string) (string, string) {
	// Register
	regPayload := map[string]interface{}{
		"email":        email,
		"password":     password,
		"phone":        fmt.Sprintf("+1%010d", time.Now().UnixNano()%10000000000),
		"first_name":   "Verify",
		"last_name":    "Admin",
		"user_type":    "individual", // Admin starts as individual usually
		"country_code": "US",
	}
	// Try register, ignore if already exists (for re-login)
	request("POST", "/auth/register", "", regPayload)

	// Login
	loginPayload := map[string]interface{}{
		"email":       email,
		"password":    password,
		"device_id":   "verify-device",
		"device_name": "Verify Script",
	}
	resp := request("POST", "/auth/login", "", loginPayload)
	if resp.StatusCode != 200 {
		log.Fatalf("Login failed: %d %s", resp.StatusCode, readBody(resp))
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	token := result["access_token"].(string)

	// Get User ID
	meResp := request("GET", "/auth/me", token, nil)
	var meResult map[string]interface{}
	json.NewDecoder(meResp.Body).Decode(&meResult)

	var userID string
	if id, ok := meResult["id"].(string); ok {
		userID = id
	} else if user, ok := meResult["user"].(map[string]interface{}); ok {
		userID = user["id"].(string)
	}

	return token, userID
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
