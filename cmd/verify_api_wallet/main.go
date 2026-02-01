package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const baseURL = "http://localhost:8080"

func main() {
	fmt.Println("=== Verifying John Doe's Wallet via API ===")

	// 1. Login
	token, err := login("john.doe@example.com", "password123")
	if err != nil {
		log.Fatalf("Login failed: %v. Make sure the API server is running on %s", err, baseURL)
	}
	fmt.Println("Login successful")

	// 2. Get Wallets
	wallets, err := getWallets(token)
	if err != nil {
		log.Fatalf("Get Wallets failed: %v", err)
	}

	// 3. Verify Currency and Balance
	found := false
	for _, w := range wallets {
		fmt.Printf("Wallet ID: %s, Currency: %s, Balance: %.2f\n", w.ID, w.Currency, w.Balance)
		if w.Currency == "MWK" {
			found = true
			if w.Balance == 500000.00 {
				fmt.Println("SUCCESS: Found MWK wallet with correct balance (500,000.00)")
			} else {
				fmt.Printf("WARNING: Found MWK wallet but balance is %.2f (Expected 500,000.00)\n", w.Balance)
			}
		}
	}

	if !found {
		fmt.Println("FAILURE: No MWK wallet found for John Doe")
	} else {
		fmt.Println("=== Verification Complete ===")
	}
}

func login(email, password string) (string, error) {
	data := map[string]string{
		"email":    email,
		"password": password,
	}
	jsonData, _ := json.Marshal(data)

	resp, err := http.Post(baseURL+"/auth/login", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Token, nil
}

type Wallet struct {
	ID       string  `json:"id"`
	Currency string  `json:"currency"`
	Balance  float64 `json:"balance"` // Using float for display simplicity
}

func getWallets(token string) ([]Wallet, error) {
	req, _ := http.NewRequest("GET", baseURL+"/wallets", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result []Wallet
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}
