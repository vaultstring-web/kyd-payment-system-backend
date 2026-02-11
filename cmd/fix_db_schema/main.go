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

	// Create blockchain_networks table
	fmt.Println("Creating blockchain_networks table...")
	blockchainTable := `
	CREATE TABLE IF NOT EXISTS customer_schema.blockchain_networks (
		network_id VARCHAR(50) PRIMARY KEY,
		name VARCHAR(100) NOT NULL,
		status VARCHAR(20) NOT NULL,
		block_height BIGINT DEFAULT 0,
		peer_count INT DEFAULT 0,
		last_block_time TIMESTAMP,
		channel VARCHAR(50) DEFAULT 'default',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`
	_, err = db.Exec(blockchainTable)
	if err != nil {
		log.Printf("⚠️  Failed to create blockchain_networks table: %v", err)
	} else {
		fmt.Println("✅ blockchain_networks table created/verified.")
	}

	fmt.Println("🎉 Schema Fix Complete!")
}
