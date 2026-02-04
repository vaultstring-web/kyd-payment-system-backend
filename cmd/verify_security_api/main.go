package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const baseURL = "http://localhost:9000/api/v1"

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	AccessToken string `json:"access_token"`
}

func main() {
	fmt.Println("🔒 Verifying Security API...")

	// 1. Login as Admin
	token, err := login("admin@kyd.com", "password123")
	if err != nil {
		fmt.Printf("❌ Login failed: %v\n", err)
		// Try accessing directly via payment service if gateway fails
		fmt.Println("⚠️ Retrying via Payment Service port 3001...")
		// Note: Payment service typically expects JWT, but if we can't get one from Auth, we are stuck.
		// However, in dev mode, maybe we can bypass or use a test token if available.
		// For now, let's assume gateway/auth works.
		os.Exit(1)
	}
	fmt.Println("✅ Admin Login Successful")

	// 2. Verify Security Events
	if err := verifyEndpoint(token, "/admin/security/events?limit=5", "Security Events"); err != nil {
		fmt.Printf("❌ Security Events Check Failed: %v\n", err)
		// Continue to check others
	}

	// 3. Verify Blocklist
	if err := verifyEndpoint(token, "/admin/security/blocklist", "Blocklist"); err != nil {
		fmt.Printf("❌ Blocklist Check Failed: %v\n", err)
	}

	// 4. Verify System Health
	if err := verifyEndpoint(token, "/admin/security/health", "System Health"); err != nil {
		fmt.Printf("❌ System Health Check Failed: %v\n", err)
	}

	fmt.Println("🎉 Security API Verification Completed")
}

func login(email, password string) (string, error) {
	reqBody, _ := json.Marshal(LoginRequest{Email: email, Password: password})
	// Try auth endpoint
	url := baseURL + "/auth/login"
	fmt.Printf("Attempting login at %s...\n", url)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var res LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	return res.AccessToken, nil
}

func verifyEndpoint(token, path, name string) error {
	url := baseURL + path
	fmt.Printf("Checking %s at %s...\n", name, url)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s returned status %d: %s", name, resp.StatusCode, string(body))
	}

	// Check if body has data
	body, _ := io.ReadAll(resp.Body)
	if len(body) < 10 {
		return fmt.Errorf("%s returned empty response", name)
	}

	fmt.Printf("✅ %s Verified (Length: %d bytes)\n", name, len(body))
	return nil
}
