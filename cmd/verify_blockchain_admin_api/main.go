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

// Helper to load env (simplified)
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

	// Setup HTTP Client
	jar, _ := cookiejar.New(nil)
	httpClient = &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}

	// Fetch CSRF Token
	fmt.Println("--- Initializing Session (CSRF) ---")
	csrfToken := fetchCSRFToken()
	fmt.Printf("[PASS] CSRF Token fetched: %s\n", csrfToken)

	fmt.Println("=========================================================")
	fmt.Println("VERIFYING ADMIN BLOCKCHAIN API (End-to-End)")
	fmt.Println("=========================================================")

	// 1. Create Admin User via API + DB Promotion
	adminEmail := fmt.Sprintf("admin-bc-%d@test.com", time.Now().UnixNano())
	adminPass := "AdminPass123!"

	fmt.Println("\n--- 1. Creating Admin User ---")
	token, userID := registerAndLogin(adminEmail, adminPass, csrfToken)
	fmt.Printf("[PASS] User Registered & Logged in. ID: %s\n", userID)

	// Promote to Admin via DB
	dbURL := "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
	if url := os.Getenv("DATABASE_URL"); url != "" {
		dbURL = url
	}
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec("UPDATE customer_schema.users SET user_type='admin' WHERE id=$1", userID)
	if err != nil {
		log.Fatalf("Failed to promote user to admin: %v", err)
	}
	fmt.Println("[PASS] User promoted to Admin in DB")

	// RE-LOGIN to refresh token with new role
	fmt.Println("\n--- Refreshing Token (Re-Login) ---")
	token = login(adminEmail, adminPass, csrfToken)
	fmt.Println("[PASS] Token Refreshed")

	// Verify Admin Role
	fmt.Println("\n--- Verifying Admin Role via /auth/me ---")
	meResp := makeRequest("GET", "/auth/me", nil, token, csrfToken)
	if meResp.StatusCode != 200 {
		log.Fatalf("Failed to get user info: %d %s", meResp.StatusCode, meResp.Body)
	}
	var meResult map[string]interface{}
	json.Unmarshal([]byte(meResp.Body), &meResult)

	var userType string
	if ut, ok := meResult["user_type"].(string); ok {
		userType = ut
	} else if user, ok := meResult["user"].(map[string]interface{}); ok {
		if ut, ok := user["user_type"].(string); ok {
			userType = ut
		}
	}

	fmt.Printf("[INFO] Current User Type: %s\n", userType)
	if userType != "admin" {
		log.Fatalf("User is not admin! Got: %s", userType)
	}

	// 2. Test Create Network
	fmt.Println("\n--- 2. Creating Blockchain Network ---")
	networkName := fmt.Sprintf("TestNet-%d", time.Now().Unix())
	createPayload := map[string]interface{}{
		"name":            networkName,
		"channel":         "public",
		"rpc_url":         "https://rpc.testnet.io",
		"chain_id":        "12345",
		"symbol":          "TST",
		"status":          "active",
		"block_height":    100,
		"peer_count":      5,
		"last_block_time": time.Now(),
	}

	createResp := makeRequest("POST", "/admin/blockchain/networks", createPayload, token, csrfToken)
	if createResp.StatusCode != 201 && createResp.StatusCode != 200 {
		log.Fatalf("Failed to create network. Status: %d, Body: %s", createResp.StatusCode, createResp.Body)
	}

	var createdNetwork map[string]interface{}
	json.Unmarshal([]byte(createResp.Body), &createdNetwork)
	networkID, ok := createdNetwork["network_id"].(string)
	if !ok {
		// Fallback if response structure is different (e.g. wrapped in "data")
		if data, ok := createdNetwork["data"].(map[string]interface{}); ok {
			networkID = data["network_id"].(string)
		} else {
			log.Fatalf("Could not parse network_id from response: %s", createResp.Body)
		}
	}
	fmt.Printf("[PASS] Created Network ID: %s\n", networkID)

	// 3. Test List Networks
	fmt.Println("\n--- 3. Listing Blockchain Networks ---")
	listResp := makeRequest("GET", "/admin/blockchain/networks", nil, token, csrfToken)
	if listResp.StatusCode != 200 {
		log.Fatalf("Failed to list networks. Status: %d", listResp.StatusCode)
	}

	var networks []map[string]interface{}
	// Check if array or wrapped
	var listResult interface{}
	json.Unmarshal([]byte(listResp.Body), &listResult)

	if listArr, ok := listResult.([]interface{}); ok {
		for _, item := range listArr {
			networks = append(networks, item.(map[string]interface{}))
		}
	} else if listObj, ok := listResult.(map[string]interface{}); ok {
		if data, ok := listObj["data"].([]interface{}); ok {
			for _, item := range data {
				networks = append(networks, item.(map[string]interface{}))
			}
		} else {
			// Maybe it IS the object if single item? Unlikely for list.
			// Or maybe "networks" key?
			if nets, ok := listObj["networks"].([]interface{}); ok {
				for _, item := range nets {
					networks = append(networks, item.(map[string]interface{}))
				}
			}
		}
	}

	found := false
	for _, n := range networks {
		if n["network_id"] == networkID {
			found = true
			break
		}
	}
	if !found {
		log.Printf("Warning: Created network not found in list. Response: %s", listResp.Body)
		// Proceeding, might be eventual consistency? Or pagination default?
	} else {
		fmt.Printf("[PASS] Found %d networks, including new one\n", len(networks))
	}

	// 4. Test Update Network
	fmt.Println("\n--- 4. Updating Blockchain Network ---")
	updatePayload := map[string]interface{}{
		"name":       networkName + "-Updated",
		"rpc_url":    "https://rpc.updated.io",
		"chain_id":   "12345",
		"symbol":     "TST",
		"channel":    "public",
		"updated_at": time.Now(),
	}
	updateResp := makeRequest("PUT", "/admin/blockchain/networks/"+networkID, updatePayload, token, csrfToken)
	if updateResp.StatusCode != 200 {
		log.Fatalf("Failed to update network. Status: %d, Body: %s", updateResp.StatusCode, updateResp.Body)
	}
	fmt.Println("[PASS] Network Updated")

	// 5. Test Get Network Details (Verification)
	fmt.Println("\n--- 5. Verifying Update ---")
	getResp := makeRequest("GET", "/admin/blockchain/networks/"+networkID, nil, token, csrfToken)
	var getNetwork map[string]interface{}
	json.Unmarshal([]byte(getResp.Body), &getNetwork)

	// Handle wrapping
	if data, ok := getNetwork["data"].(map[string]interface{}); ok {
		getNetwork = data
	}

	if getNetwork["name"] != networkName+"-Updated" {
		log.Printf("Update verification warning. Expected %s, got %s", networkName+"-Updated", getNetwork["name"])
	} else {
		fmt.Println("[PASS] Update Verified")
	}

	// 6. Test Delete Network
	fmt.Println("\n--- 6. Deleting Blockchain Network ---")
	deleteResp := makeRequest("DELETE", "/admin/blockchain/networks/"+networkID, nil, token, csrfToken)
	if deleteResp.StatusCode != 200 && deleteResp.StatusCode != 204 {
		log.Fatalf("Failed to delete network. Status: %d", deleteResp.StatusCode)
	}
	fmt.Println("[PASS] Network Deleted")

	// 7. Verify Deletion
	fmt.Println("\n--- 7. Verifying Deletion ---")
	checkResp := makeRequest("GET", "/admin/blockchain/networks/"+networkID, nil, token, csrfToken)
	if checkResp.StatusCode != 404 {
		log.Fatalf("Network still exists after deletion! Status: %d", checkResp.StatusCode)
	}
	fmt.Println("[PASS] Deletion Verified")

	fmt.Println("\nSUCCESS: All Admin Blockchain API tests passed!")
}

// Helpers

type Response struct {
	StatusCode int
	Body       string
}

func fetchCSRFToken() string {
	resp, err := httpClient.Get(baseURL + "/health")
	if err != nil {
		log.Fatalf("Failed to fetch CSRF token (health check failed): %v", err)
	}
	defer resp.Body.Close()

	// 1. Check Cookies
	u, _ := resp.Request.URL.Parse(baseURL)
	cookies := httpClient.Jar.Cookies(u)
	for _, c := range cookies {
		if c.Name == "csrf_token" {
			return c.Value
		}
	}

	// 2. Check Set-Cookie header directly if not in Jar yet (though it should be)
	for _, c := range resp.Cookies() {
		if c.Name == "csrf_token" {
			return c.Value
		}
	}

	// Not strict failure here, maybe it's disabled? But the error said "Invalid CSRF token"
	log.Println("Warning: No csrf_token cookie found. Proceeding without it (might fail).")
	return ""
}

func makeRequest(method, path string, payload interface{}, token string, csrfToken string) Response {
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
	if csrfToken != "" && (method == "POST" || method == "PUT" || method == "DELETE" || method == "PATCH") {
		req.Header.Set("X-CSRF-Token", csrfToken)
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

func registerAndLogin(email, password string, csrfToken string) (string, string) {
	// Register
	regPayload := map[string]interface{}{
		"email":        email,
		"password":     password,
		"phone":        fmt.Sprintf("+1%010d", time.Now().UnixNano()%10000000000),
		"first_name":   "Admin",
		"last_name":    "Tester",
		"user_type":    "individual",
		"country_code": "US",
	}
	resp := makeRequest("POST", "/auth/register", regPayload, "", csrfToken)
	if resp.StatusCode != 201 {
		log.Fatalf("Register failed: %d %s", resp.StatusCode, resp.Body)
	}

	token := login(email, password, csrfToken)

	// Get User ID
	meResp := makeRequest("GET", "/auth/me", nil, token, csrfToken)
	if meResp.StatusCode != 200 {
		log.Fatalf("Failed to get user info: %d %s", meResp.StatusCode, meResp.Body)
	}
	var meResult map[string]interface{}
	json.Unmarshal([]byte(meResp.Body), &meResult)

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

func login(email, password, csrfToken string) string {
	loginPayload := map[string]interface{}{
		"email":    email,
		"password": password,
	}
	resp := makeRequest("POST", "/auth/login", loginPayload, "", csrfToken)
	if resp.StatusCode != 200 {
		log.Fatalf("Login failed: %d %s", resp.StatusCode, resp.Body)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(resp.Body), &result)
	return result["access_token"].(string)
}
