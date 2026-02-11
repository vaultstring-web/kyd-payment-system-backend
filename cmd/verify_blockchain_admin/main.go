package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Config
const (
	GatewayURL = "http://localhost:9000"
	AdminEmail = "admin@kyd.com"
	AdminPass  = "password123"
)

// BlockchainNetworkInfo matches the struct in the backend
type BlockchainNetworkInfo struct {
	ID            string    `json:"network_id"`
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	Status        string    `json:"status"`
	BlockHeight   int64     `json:"height"`
	PeerCount     int       `json:"peer_count"`
	LastBlockTime time.Time `json:"last_block_time"`
	Consensus     string    `json:"consensus"`
	Version       string    `json:"version"`
	Channel       string    `json:"channel"`
}

func main() {
	// Disable SSL verification for local testing if needed (though we use http here)
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	fmt.Println("Starting Admin Blockchain Network Verification...")

	// 1. Login as Admin to get Token
	token, err := loginAdmin()
	if err != nil {
		log.Fatalf("Login failed: %v", err)
	}
	fmt.Println("✅ Admin Login Successful")
	fmt.Printf("Token len: %d\n", len(token))
	fmt.Printf("Token: %s\n", token)

	// 2. Create a new Blockchain Network
	networkID := uuid.New().String()
	newNetwork := BlockchainNetworkInfo{
		ID:            networkID,
		Name:          "Test Network " + networkID[:8],
		Type:          "private",
		Status:        "active",
		BlockHeight:   100,
		PeerCount:     5,
		LastBlockTime: time.Now(),
		Consensus:     "raft",
		Version:       "1.0.0",
		Channel:       "default",
	}

	createdNetwork, err := createNetwork(token, newNetwork)
	if err != nil {
		log.Fatalf("Create Network failed: %v", err)
	}
	fmt.Printf("✅ Network Created: ID=%s, Name=%s\n", createdNetwork.ID, createdNetwork.Name)

	// 3. Update the Network (PATCH)
	updates := map[string]interface{}{
		"name":       "Updated Test Network",
		"peer_count": 10,
		"status":     "maintenance",
	}
	updatedNetwork, err := updateNetwork(token, createdNetwork.ID, updates)
	if err != nil {
		log.Fatalf("Update Network failed: %v", err)
	}

	if updatedNetwork.Name != "Updated Test Network" || updatedNetwork.PeerCount != 10 || updatedNetwork.Status != "maintenance" {
		log.Fatalf("Update verification failed. Got: %+v", updatedNetwork)
	}
	fmt.Println("✅ Network Updated (PATCH) Successfully")

	// 4. Get Network Details
	fetchedNetwork, err := getNetwork(token, createdNetwork.ID)
	if err != nil {
		log.Fatalf("Get Network failed: %v", err)
	}
	if fetchedNetwork.ID != createdNetwork.ID {
		log.Fatalf("Fetched network ID mismatch")
	}
	fmt.Println("✅ Get Network Details Successful")

	// 5. List All Networks
	networks, err := listNetworks(token)
	if err != nil {
		log.Fatalf("List Networks failed: %v", err)
	}
	found := false
	for _, n := range networks {
		if n.ID == createdNetwork.ID {
			found = true
			break
		}
	}
	if !found {
		log.Fatalf("Created network not found in list")
	}
	fmt.Printf("✅ List Networks Successful (Found %d networks)\n", len(networks))

	// 6. Delete Network
	if err := deleteNetwork(token, createdNetwork.ID); err != nil {
		log.Fatalf("Delete Network failed: %v", err)
	}
	fmt.Println("✅ Delete Network Successful")

	// Verify Deletion
	_, err = getNetwork(token, createdNetwork.ID)
	if err == nil {
		log.Fatalf("Network should have been deleted but was found")
	}
	fmt.Println("✅ Deletion Verified")

	fmt.Println("🎉 All Blockchain Admin Verifications Passed!")
}

// Helper Functions

func loginAdmin() (string, error) {
	loginData := map[string]string{
		"email":    AdminEmail,
		"password": AdminPass,
	}
	jsonData, _ := json.Marshal(loginData)

	resp, err := http.Post(GatewayURL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("login failed status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Token, nil
}

func createNetwork(token string, network BlockchainNetworkInfo) (*BlockchainNetworkInfo, error) {
	jsonData, _ := json.Marshal(network)
	req, _ := http.NewRequest("POST", GatewayURL+"/api/v1/admin/blockchain/networks", bytes.NewBuffer(jsonData))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	// CSRF Bypass for verification script
	csrfToken := "test-csrf-token-123"
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
	req.Header.Set("X-CSRF-Token", csrfToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create failed status %d: %s", resp.StatusCode, string(body))
	}

	var created BlockchainNetworkInfo
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, err
	}
	return &created, nil
}

func updateNetwork(token, id string, updates map[string]interface{}) (*BlockchainNetworkInfo, error) {
	jsonData, _ := json.Marshal(updates)
	// Using PATCH as verified in previous step
	req, _ := http.NewRequest("PATCH", fmt.Sprintf("%s/api/v1/admin/blockchain/networks/%s", GatewayURL, id), bytes.NewBuffer(jsonData))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	// CSRF Bypass
	csrfToken := "test-csrf-token-123"
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
	req.Header.Set("X-CSRF-Token", csrfToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("update failed status %d: %s", resp.StatusCode, string(body))
	}

	var updated BlockchainNetworkInfo
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func getNetwork(token, id string) (*BlockchainNetworkInfo, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/admin/blockchain/networks/%s", GatewayURL, id), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	// CSRF Bypass
	csrfToken := "test-csrf-token-123"
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
	req.Header.Set("X-CSRF-Token", csrfToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get failed status %d", resp.StatusCode)
	}

	var network BlockchainNetworkInfo
	if err := json.NewDecoder(resp.Body).Decode(&network); err != nil {
		return nil, err
	}
	return &network, nil
}

func listNetworks(token string) ([]BlockchainNetworkInfo, error) {
	req, _ := http.NewRequest("GET", GatewayURL+"/api/v1/admin/blockchain/networks", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	// CSRF Bypass
	csrfToken := "test-csrf-token-123"
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
	req.Header.Set("X-CSRF-Token", csrfToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list failed status %d", resp.StatusCode)
	}

	var result struct {
		Networks []BlockchainNetworkInfo `json:"networks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Networks, nil
}

func deleteNetwork(token, id string) error {
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/admin/blockchain/networks/%s", GatewayURL, id), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	// CSRF Bypass
	csrfToken := "test-csrf-token-123"
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
	req.Header.Set("X-CSRF-Token", csrfToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
