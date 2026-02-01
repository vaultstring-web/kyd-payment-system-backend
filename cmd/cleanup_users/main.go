package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	"kyd/internal/security"

	_ "github.com/lib/pq"
)

func main() {
	// Initialize security service for blind index generation
	cryptoService, err := security.NewCryptoService()
	if err != nil {
		log.Fatalf("Failed to initialize crypto service: %v", err)
	}

	emails := []string{
		"admin@example.com",
		"john.doe@example.com",
		"jane.smith@example.com",
		"new.user@example.com",
		"alice.mw.001@example.com",
	}

	// Compute blind indexes for emails
	emailHashes := make([]string, len(emails))
	for i, email := range emails {
		emailHashes[i] = cryptoService.BlindIndex(email)
	}

	{
		u := "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
		db, err := sql.Open("postgres", u)
		if err == nil {
			defer db.Close()
			_, _ = db.Exec(`GRANT USAGE ON SCHEMA customer_schema TO kyd_admin`)
			_, _ = db.Exec(`GRANT USAGE ON SCHEMA admin_schema TO kyd_admin`)
			_, _ = db.Exec(`GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA customer_schema TO kyd_admin`)
			_, _ = db.Exec(`GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA admin_schema TO kyd_admin`)
		}
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://kyd_admin:admin_secure_pass@localhost:5432/kyd_dev?sslmode=disable"
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("db open error: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("db ping error: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		log.Fatalf("tx begin error: %v", err)
	}

	ph := make([]string, len(emailHashes))
	args := make([]interface{}, len(emailHashes))
	for i, hash := range emailHashes {
		ph[i] = fmt.Sprintf("$%d", i+1)
		args[i] = hash
	}
	notIn := "NOT IN (" + strings.Join(ph, ",") + ")"

	// Use email_hash for lookups since email is encrypted
	ledgerSQL := fmt.Sprintf(`
		DELETE FROM customer_schema.ledger_entries
		WHERE transaction_id IN (
			SELECT id FROM customer_schema.transactions
			WHERE sender_id IN (
				SELECT id FROM customer_schema.users WHERE email_hash %s
			)
			OR receiver_id IN (
				SELECT id FROM customer_schema.users WHERE email_hash %s
			)
		)
	`, notIn, notIn)
	if _, err := tx.Exec(ledgerSQL, args...); err != nil {
		_ = tx.Rollback()
		log.Fatalf("delete ledger_entries error: %v", err)
	}

	txSQL := fmt.Sprintf(`
		DELETE FROM customer_schema.transactions
		WHERE sender_id IN (
			SELECT id FROM customer_schema.users WHERE email_hash %s
		)
		OR receiver_id IN (
			SELECT id FROM customer_schema.users WHERE email_hash %s
		)
	`, notIn, notIn)
	if _, err := tx.Exec(txSQL, args...); err != nil {
		_ = tx.Rollback()
		log.Fatalf("delete transactions error: %v", err)
	}

	auditSQL := fmt.Sprintf(`
		DELETE FROM admin_schema.audit_logs
		WHERE user_id IN (
			SELECT id FROM customer_schema.users WHERE email_hash %s
		)
	`, notIn)
	if _, err := tx.Exec(auditSQL, args...); err != nil {
		_ = tx.Rollback()
		log.Fatalf("delete audit_logs error: %v", err)
	}

	// Delete wallets for users NOT in the list
	walletSQL := fmt.Sprintf(`
		DELETE FROM customer_schema.wallets
		WHERE user_id IN (
			SELECT id FROM customer_schema.users WHERE email_hash %s
		)
	`, notIn)
	if _, err := tx.Exec(walletSQL, args...); err != nil {
		_ = tx.Rollback()
		log.Fatalf("delete wallets error: %v", err)
	}

	// Delete users NOT in the list
	userSQL := fmt.Sprintf(`
		DELETE FROM customer_schema.users
		WHERE email_hash %s
	`, notIn)
	res, err := tx.Exec(userSQL, args...)
	if err != nil {
		_ = tx.Rollback()
		log.Fatalf("delete users error: %v", err)
	}
	affected, _ := res.RowsAffected()

	if err := tx.Commit(); err != nil {
		log.Fatalf("tx commit error: %v", err)
	}

	fmt.Printf("Deleted %d users\n", affected)

	rows, err := db.Query(`SELECT email, user_type, kyc_status FROM customer_schema.users ORDER BY email`)
	if err != nil {
		log.Fatalf("list users error: %v", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var email, userType, kyc string
		if err := rows.Scan(&email, &userType, &kyc); err != nil {
			log.Fatalf("scan error: %v", err)
		}
		out = append(out, fmt.Sprintf("%s %s %s", email, userType, strings.Title(kyc)))
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("rows err: %v", err)
	}

	fmt.Println("Remaining users:")
	for _, line := range out {
		fmt.Println(line)
	}
}
