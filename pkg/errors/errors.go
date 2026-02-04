// Package errors provides common, reusable error values and helpers.
package errors

import (
	"errors"
	"fmt"
)

// Common errors
var (
	ErrUserNotFound             = errors.New("user not found")
	ErrUserAlreadyExists        = errors.New("user already exists")
	ErrInvalidCredentials       = errors.New("invalid credentials")
	ErrWalletNotFound           = errors.New("wallet not found")
	ErrWalletAlreadyExists      = errors.New("wallet already exists")
	ErrInsufficientBalance      = errors.New("insufficient balance")
	ErrTransactionNotFound      = errors.New("transaction not found")
	ErrTransactionAlreadyExists = errors.New("transaction already exists")
	ErrDuplicateRequest         = errors.New("Duplicate request")
	ErrSettlementNotFound       = errors.New("settlement not found")
	ErrRateNotAvailable         = errors.New("exchange rate not available")
	ErrCurrencyNotAllowed       = errors.New("currency not allowed for user country")
)

// New returns a new error with the given text
func New(text string) error {
	return errors.New(text)
}

// Wrap wraps an error with additional context
func Wrap(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}
