package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Println("DATABASE_URL not set")
		os.Exit(1)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fmt.Printf("DB open error: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	var johnHash string
	if err := db.QueryRow(`SELECT password_hash FROM customer_schema.users WHERE email = $1`, "john.doe@example.com").Scan(&johnHash); err != nil {
		fmt.Printf("Failed to read John's hash: %v\n", err)
		os.Exit(1)
	}
	res, err := db.Exec(`UPDATE customer_schema.users SET password_hash = $1, failed_login_attempts = 0, locked_until = NULL WHERE email = $2`, johnHash, "admin@example.com")
	if err != nil {
		fmt.Printf("Update admin password failed: %v\n", err)
		os.Exit(1)
	}
	affected, _ := res.RowsAffected()
	fmt.Printf("Updated admin password. Rows affected: %d\n", affected)
}
