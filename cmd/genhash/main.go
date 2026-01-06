package main

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	password := "Password123"
	// The hash we put in DB
	hashStr := "$2a$10$G0q6dqztm4crtpd/XtwhoeYBiNJrIYKtddSjNthCt9sNP5px20OW6"

	err := bcrypt.CompareHashAndPassword([]byte(hashStr), []byte(password))
	if err != nil {
		fmt.Printf("Verification FAILED: %v\n", err)
	} else {
		fmt.Println("Verification SUCCESS")
	}
}
