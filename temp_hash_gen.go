package main

import (
	"fmt"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	hashedPwd, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		fmt.Println("Error hashing password:", err)
		return
	}
	fmt.Println(string(hashedPwd))
}
