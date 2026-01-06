package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/lib/pq"
)

func main() {
	emails := []string{
		"admin@example.com",
		"john.doe@example.com",
		"jane.smith@example.com",
		"new.user@example.com",
		"alice.mw.001@example.com",
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

	ph := make([]string, len(emails))
	args := make([]interface{}, len(emails))
	for i, e := range emails {
		ph[i] = fmt.Sprintf("$%d", i+1)
		args[i] = e
	}
	notIn := "NOT IN (" + strings.Join(ph, ",") + ")"

	ledgerSQL := fmt.Sprintf(`
		DELETE FROM customer_schema.ledger_entries
		WHERE transaction_id IN (
			SELECT id FROM customer_schema.transactions
			WHERE sender_id IN (
				SELECT id FROM customer_schema.users WHERE email %s
			)
			OR receiver_id IN (
				SELECT id FROM customer_schema.users WHERE email %s
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
			SELECT id FROM customer_schema.users WHERE email %s
		)
		OR receiver_id IN (
			SELECT id FROM customer_schema.users WHERE email %s
		)
	`, notIn, notIn)
	if _, err := tx.Exec(txSQL, args...); err != nil {
		_ = tx.Rollback()
		log.Fatalf("delete transactions error: %v", err)
	}

	auditSQL := fmt.Sprintf(`
		DELETE FROM admin_schema.audit_logs
		WHERE user_id IN (
			SELECT id FROM customer_schema.users WHERE email %s
		)
	`, notIn)
	if _, err := tx.Exec(auditSQL, args...); err != nil {
		_ = tx.Rollback()
		log.Fatalf("delete audit_logs error: %v", err)
	}

	usersSQL := fmt.Sprintf(`
		DELETE FROM customer_schema.users
		WHERE email %s
	`, notIn)
	res, err := tx.Exec(usersSQL, args...)
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
