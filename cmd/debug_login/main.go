package main

import (
	"fmt"
	"log"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	dbURL := "postgres://kyd_system:system_secure_pass@localhost:5432/kyd_dev?sslmode=disable&search_path=customer_schema,admin_schema,audit_schema,public"
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	email := "new.user@example.com"
	password := "Password123"

	var user struct {
		ID           string `db:"id"`
		Email        string `db:"email"`
		PasswordHash string `db:"password_hash"`
	}

	err = db.Get(&user, "SELECT id, email, password_hash FROM customer_schema.users WHERE email = $1", email)
	if err != nil {
		log.Fatalf("User not found: %v", err)
	}

	fmt.Printf("User: %s\n", user.Email)
	fmt.Printf("Hash: %s\n", user.PasswordHash)

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		fmt.Printf("Compare FAILED: %v\n", err)
	} else {
		fmt.Println("Compare SUCCESS")
	}
}
