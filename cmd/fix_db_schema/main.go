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
	fmt.Println("🔧 Fixing Database Schema...")

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

	// Add email_blind_index column
	fmt.Println("Adding email_blind_index column...")
	_, err = db.Exec("ALTER TABLE customer_schema.users ADD COLUMN IF NOT EXISTS email_blind_index VARCHAR(255)")
	if err != nil {
		log.Printf("⚠️  Failed to add column (might already exist): %v", err)
	} else {
		fmt.Println("✅ Column added.")
	}

	// Add index
	fmt.Println("Adding index for email_blind_index...")
	_, err = db.Exec("CREATE INDEX IF NOT EXISTS idx_users_email_blind_index ON customer_schema.users(email_blind_index)")
	if err != nil {
		log.Printf("⚠️  Failed to add index: %v", err)
	} else {
		fmt.Println("✅ Index added.")
	}

	fmt.Println("🎉 Schema Fix Complete!")
}
