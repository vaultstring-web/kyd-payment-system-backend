package domain

import (
	"time"

	"github.com/google/uuid"
)

// UserDevice represents a user's trusted device
type UserDevice struct {
	ID          uuid.UUID `json:"id" db:"id"`
	UserID      uuid.UUID `json:"user_id" db:"user_id"`
	DeviceHash  string    `json:"device_hash" db:"device_hash"`
	DeviceName  *string   `json:"device_name,omitempty" db:"device_name"`
	CountryCode *string   `json:"country_code,omitempty" db:"country_code"`
	IPAddress   *string   `json:"ip_address,omitempty" db:"ip_address"`
	IsTrusted   bool      `json:"is_trusted" db:"is_trusted"`
	LastSeenAt  time.Time `json:"last_seen_at" db:"last_seen_at"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}
