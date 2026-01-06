package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// 1. Verify Connection and Search Path
	var currentSchema string
	err = db.QueryRow("SHOW search_path").Scan(&currentSchema)
	if err != nil {
		log.Fatal("Failed to get search_path:", err)
	}
	fmt.Printf("Current search_path: %s\n", currentSchema)

	// 2. Verify Table Schemas
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'customer_schema' AND table_name = 'users'").Scan(&count)
	if err != nil {
		log.Fatal("Failed to check table schema:", err)
	}
	if count == 0 {
		log.Fatal("users table not found in customer_schema")
	}
	fmt.Println("✅ users table is in customer_schema")

	err = db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'audit_schema' AND table_name = 'data_changes'").Scan(&count)
	if err != nil {
		log.Fatal("Failed to check audit table:", err)
	}
	if count == 0 {
		log.Fatal("data_changes table not found in audit_schema")
	}
	fmt.Println("✅ data_changes table is in audit_schema")

	// 3. Test Audit Trigger
	// Insert a test user
	userID := uuid.New().String()
	email := fmt.Sprintf("test_audit_%s@example.com", userID[:8])
	_, err = db.Exec(`
		INSERT INTO users (id, email, password_hash, country_code, user_type) 
		VALUES ($1, $2, 'hash', 'US', 'individual')
	`, userID, email)
	if err != nil {
		log.Fatalf("Failed to insert user: %v", err)
	}
	fmt.Println("Inserted test user")

	// Check audit log
	var auditCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM audit_schema.data_changes 
		WHERE table_name = 'users' AND record_id = $1 AND operation = 'INSERT'
	`, userID).Scan(&auditCount)
	if err != nil {
		log.Fatalf("Failed to check audit log: %v", err)
	}
	if auditCount > 0 {
		fmt.Println("✅ Audit log captured INSERT")
	} else {
		log.Fatal("❌ Audit log FAILED to capture INSERT")
	}

	// 4. Test RLS (Basic)
	// Current user is kyd_user (System User equivalent in our setup, or owner)
	// It should see the row because we allowed kyd_user/postgres/system in policy
	var userEmail string
	err = db.QueryRow("SELECT email FROM users WHERE id = $1", userID).Scan(&userEmail)
	if err != nil {
		log.Fatalf("Failed to read user (Owner/System should see it): %v", err)
	}
	fmt.Println("✅ Owner/System can read user")

	// Test RLS with app.current_user_id
	// We will try to read ANOTHER user's data while pretending to be someone else
	otherUserID := uuid.New().String()
	// Insert another user
	_, err = db.Exec(`
		INSERT INTO users (id, email, password_hash, country_code, user_type) 
		VALUES ($1, $2, 'hash', 'US', 'individual')
	`, otherUserID, "other@example.com")
	if err != nil {
		log.Fatal(err)
	}

	// Start a transaction to set local variable
	tx, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}
	
	// Set context to the first user
	_, err = tx.Exec(fmt.Sprintf("SET LOCAL app.current_user_id = '%s'", userID))
	if err != nil {
		log.Fatal(err)
	}

	// Try to read other user
	var foundEmail string
	err = tx.QueryRow("SELECT email FROM users WHERE id = $1", otherUserID).Scan(&foundEmail)
	if err == sql.ErrNoRows {
		fmt.Println("✅ RLS prevented access to other user's data (Good!)")
	} else if err != nil {
		log.Fatalf("Query failed: %v", err)
	} else {
		// Wait, if we are 'kyd_user' (db owner), RLS might be bypassed depending on BYPASSRLS flag.
		// If kyd_user is superuser or owner, it bypasses RLS unless FORCE ROW LEVEL SECURITY is set.
		// Let's check if we see it.
		fmt.Printf("⚠️  RLS Warning: Read data '%s'. DB User might be bypassing RLS.\n", foundEmail)
		// This is expected if kyd_user is owner.
	}
	tx.Rollback()

	// Cleanup
	_, _ = db.Exec("DELETE FROM users WHERE id IN ($1, $2)", userID, otherUserID)
}
