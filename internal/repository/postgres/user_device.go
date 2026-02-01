package postgres

import (
	"context"
	"database/sql"

	"kyd/internal/domain"

	"github.com/google/uuid"
)

func (r *UserRepository) AddDevice(ctx context.Context, device *domain.UserDevice) error {
	query := `
		INSERT INTO customer_schema.user_devices (
			user_id, device_hash, device_name, ip_address, country_code, is_trusted, last_seen_at, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
		ON CONFLICT (user_id, device_hash) DO UPDATE SET
			last_seen_at = EXCLUDED.last_seen_at,
			ip_address = EXCLUDED.ip_address,
			country_code = COALESCE(EXCLUDED.country_code, customer_schema.user_devices.country_code),
			device_name = COALESCE(EXCLUDED.device_name, customer_schema.user_devices.device_name)
	`
	_, err := r.db.ExecContext(ctx, query,
		device.UserID, device.DeviceHash, device.DeviceName, device.IPAddress, device.CountryCode,
		device.IsTrusted, device.LastSeenAt, device.CreatedAt,
	)
	return err
}

func (r *UserRepository) IsCountryTrusted(ctx context.Context, userID uuid.UUID, countryCode string) (bool, error) {
	// A country is trusted if the user has at least one trusted device from that country
	query := `
		SELECT EXISTS (
			SELECT 1 FROM customer_schema.user_devices
			WHERE user_id = $1 AND country_code = $2 AND is_trusted = true
		)
	`
	var trusted bool
	err := r.db.GetContext(ctx, &trusted, query, userID, countryCode)
	if err != nil {
		return false, err
	}
	return trusted, nil
}

func (r *UserRepository) IsDeviceTrusted(ctx context.Context, userID uuid.UUID, deviceHash string) (bool, error) {
	query := `
		SELECT is_trusted FROM customer_schema.user_devices
		WHERE user_id = $1 AND device_hash = $2
	`
	var isTrusted bool
	err := r.db.GetContext(ctx, &isTrusted, query, userID, deviceHash)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return isTrusted, nil
}
