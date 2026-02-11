package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
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
	fmt.Println("VERIFYING ADMIN COMPLIANCE/KYC WORKFLOW (End-to-End)")
	fmt.Println("=========================================================")

	// 0. Connect to DB (for cleanup or verification if needed)
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

	// 2. Create User for KYC
	fmt.Println("\n--- 2. Creating User for KYC ---")
	userEmail := fmt.Sprintf("kyc-user-%d@test.com", time.Now().UnixNano())
	userPass := "Pass123!"
	userDeviceID := "device-kyc-user"
	userToken, userID := registerAndLogin(userEmail, userPass, userDeviceID, csrfToken)
	fmt.Printf("[PASS] User Created: %s (ID: %s)\n", userEmail, userID)

	// 3. Submit KYC Application (As User)
	fmt.Println("\n--- 3. Submitting KYC Application ---")

	// Create a buffer to write our multipart form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add fields
	_ = writer.WriteField("document_type", "national_id")
	_ = writer.WriteField("document_number", fmt.Sprintf("ID-%d", time.Now().UnixNano()))
	_ = writer.WriteField("issuing_country", "MW")

	// Add file
	part, err := writer.CreateFormFile("documents", "id_card.jpg")
	if err != nil {
		log.Fatalf("Failed to create form file: %v", err)
	}
	// Write some dummy content
	part.Write([]byte("dummy image content"))

	// Close writer
	err = writer.Close()
	if err != nil {
		log.Fatalf("Failed to close writer: %v", err)
	}

	// Create Request
	req, err := http.NewRequest("POST", baseURL+"/compliance/kyc/submit", body)
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+userToken)
	req.Header.Set("X-CSRF-Token", csrfToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		log.Fatalf("KYC Submission Failed. Status: %d, Body: %s", resp.StatusCode, string(respBody))
	}

	fmt.Printf("[PASS] KYC Application Submitted. Response: %s\n", string(respBody))

	// 4. Create and Login as Admin
	fmt.Println("\n--- 4. Creating and Logging in as Admin ---")
	adminEmail := fmt.Sprintf("admin-%d@kyd.com", time.Now().UnixNano())
	adminPass := "AdminPass123!"
	adminDeviceID := "device-admin-ops"

	// Register generic user first
	regPayload := map[string]string{
		"email":        adminEmail,
		"password":     adminPass,
		"first_name":   "Admin",
		"last_name":    "User",
		"phone":        fmt.Sprintf("+26599%d", time.Now().UnixNano()%10000000),
		"country_code": "MW",
		"user_type":    "individual",
	}
	regResp := makeRequest("POST", "/auth/register", regPayload, "", csrfToken, nil)
	if regResp.StatusCode != 201 && regResp.StatusCode != 200 {
		log.Fatalf("Admin Registration failed: %d %s", regResp.StatusCode, regResp.Body)
	}

	var regResult map[string]interface{}
	json.Unmarshal([]byte(regResp.Body), &regResult)
	var adminID string
	if userMap, ok := regResult["user"].(map[string]interface{}); ok {
		adminID = userMap["id"].(string)
	} else if data, ok := regResult["data"].(map[string]interface{}); ok {
		// handle wrapper
		if userMap, ok := data["user"].(map[string]interface{}); ok {
			adminID = userMap["id"].(string)
		}
	}
	if adminID == "" {
		// Fallback: Login to get ID if register didn't return it
		_, adminID = login(adminEmail, adminPass, adminDeviceID, csrfToken)
	}

	// Promote to Admin in DB
	_, err = db.Exec("UPDATE customer_schema.users SET user_type='admin' WHERE id=$1", adminID)
	if err != nil {
		log.Fatalf("Failed to promote user to admin: %v", err)
	}
	fmt.Printf("[PASS] Promoted %s (ID: %s) to Admin\n", adminEmail, adminID)

	// Login
	adminToken, _ := login(adminEmail, adminPass, adminDeviceID, csrfToken)
	fmt.Printf("[PASS] Admin Logged In\n")

	// 5. List KYC Applications (As Admin)
	fmt.Println("\n--- 5. Listing KYC Applications ---")
	listResp := makeRequest("GET", "/admin/compliance/kyc?status=pending", nil, adminToken, csrfToken, nil)
	if listResp.StatusCode != 200 {
		log.Fatalf("Failed to list KYC applications. Status: %d, Body: %s", listResp.StatusCode, listResp.Body)
	}

	var listResult map[string]interface{}
	json.Unmarshal([]byte(listResp.Body), &listResult)

	var appID string
	if apps, ok := listResult["applications"].([]interface{}); ok {
		for _, a := range apps {
			app := a.(map[string]interface{})
			if app["user_id"] == userID {
				appID = app["id"].(string)
				break
			}
		}
	}

	if appID == "" {
		log.Fatalf("[FAIL] Newly created KYC application for user %s not found in pending list", userID)
	}
	fmt.Printf("[PASS] Found KYC Application ID: %s\n", appID)

	// 6. Review/Approve Application (As Admin)
	fmt.Println("\n--- 6. Approving KYC Application ---")
	reviewPayload := map[string]string{
		"status": "verified",
		"reason": "Automated test verification",
	}
	// Endpoint: /admin/compliance/kyc/{id}/status
	reviewResp := makeRequest("PATCH", fmt.Sprintf("/admin/compliance/kyc/%s/status", appID), reviewPayload, adminToken, csrfToken, nil)
	if reviewResp.StatusCode != 200 {
		log.Fatalf("Failed to approve KYC application. Status: %d, Body: %s", reviewResp.StatusCode, reviewResp.Body)
	}
	fmt.Printf("[PASS] KYC Application Approved\n")

	// 7. Verify Status Update (As User or Admin)
	fmt.Println("\n--- 7. Verifying Status Update ---")
	// Check user's KYC status via /auth/me or by querying DB
	var kycStatus string
	err = db.QueryRow("SELECT kyc_status FROM customer_schema.users WHERE id=$1", userID).Scan(&kycStatus)
	if err != nil {
		log.Fatalf("Failed to query user KYC status: %v", err)
	}

	if kycStatus != "verified" {
		log.Fatalf("[FAIL] User KYC status is %s, expected 'verified'", kycStatus)
	}
	fmt.Printf("[PASS] User KYC Status Verified in DB: %s\n", kycStatus)

	fmt.Println("\nSUCCESS: Admin Compliance/KYC Workflow Verified!")
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
		"first_name":   "KYC",
		"last_name":    "User",
		"phone":        fmt.Sprintf("+26599%d", time.Now().UnixNano()%10000000),
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
		log.Fatalf("Login failed for %s: %d %s", email, loginResp.StatusCode, loginResp.Body)
	}

	var loginResult map[string]interface{}
	json.Unmarshal([]byte(loginResp.Body), &loginResult)

	token, ok := loginResult["token"].(string)
	if !ok {
		token, ok = loginResult["access_token"].(string)
	}
	if !ok {
		if data, ok := loginResult["data"].(map[string]interface{}); ok {
			token = data["token"].(string)
			if token == "" {
				token = data["access_token"].(string)
			}
		}
	}
	if token == "" {
		log.Fatalf("Login response missing token. Body: %s", loginResp.Body)
	}

	var userID string
	if user, ok := loginResult["user"].(map[string]interface{}); ok {
		userID = user["id"].(string)
	} else if data, ok := loginResult["data"].(map[string]interface{}); ok {
		if user, ok := data["user"].(map[string]interface{}); ok {
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
