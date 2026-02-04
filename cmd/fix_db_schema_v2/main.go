package main

import (
	"fmt"
	"log"
	"os"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	fmt.Println("🔧 Fixing Database Schema (Correction)...")

	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, relying on environment variables")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
	}

	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Add email_hash column
	fmt.Println("Adding email_hash column...")
	_, err = db.Exec("ALTER TABLE customer_schema.users ADD COLUMN IF NOT EXISTS email_hash VARCHAR(255)")
	if err != nil {
		log.Printf("⚠️  Failed to add email_hash: %v", err)
	} else {
		fmt.Println("✅ email_hash added.")
	}

	// Add index for email_hash
	fmt.Println("Adding index for email_hash...")
	_, err = db.Exec("CREATE INDEX IF NOT EXISTS idx_users_email_hash ON customer_schema.users(email_hash)")
	if err != nil {
		log.Printf("⚠️  Failed to add index: %v", err)
	} else {
		fmt.Println("✅ Index added.")
	}
	
	// Add phone_hash column (Repository uses it too)
	fmt.Println("Adding phone_hash column...")
	_, err = db.Exec("ALTER TABLE customer_schema.users ADD COLUMN IF NOT EXISTS phone_hash VARCHAR(255)")
	if err != nil {
		log.Printf("⚠️  Failed to add phone_hash: %v", err)
	} else {
		fmt.Println("✅ phone_hash added.")
	}

	// Drop email_blind_index if it exists
	fmt.Println("Dropping email_blind_index column...")
	_, err = db.Exec("ALTER TABLE customer_schema.users DROP COLUMN IF EXISTS email_blind_index")
	if err != nil {
		log.Printf("⚠️  Failed to drop email_blind_index: %v", err)
	} else {
		fmt.Println("✅ email_blind_index dropped.")
	}

	fmt.Println("🎉 Schema Correction Complete!")
}
